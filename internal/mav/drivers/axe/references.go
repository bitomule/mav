package axe

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// --view full has a versioned schema; the default reading view does not.
type referenceEnvelope struct {
	Schema        string                          `json:"schema"`
	SchemaVersion string                          `json:"schemaVersion"`
	DidError      *bool                           `json:"didError"`
	Error         *struct{ Code, Message string } `json:"error"`
	Data          struct {
		Capture *struct {
			SnapshotID            string          `json:"snapshotID"`
			ApplicationPID        int             `json:"applicationPID"`
			Context               json.RawMessage `json:"context"`
			Targets, Scroll, Text []string
			NextRowOffset         *int              `json:"nextRowOffset"`
			UnavailableReferences map[string]string `json:"unavailableReferences"`
		} `json:"capture"`
		Action *drivers.InputReceipt `json:"action"`
		Cache  struct {
			RequiresRefresh bool `json:"requiresRefresh"`
		} `json:"cache"`
	} `json:"data"`
}

func (d *Driver) referenceRun(ctx context.Context, t drivers.Target, mutation bool, args ...string) (referenceEnvelope, drivers.ReferenceResult, error) {
	r := drivers.ReferenceResult{Delivery: "not_sent"}
	if t.Kind != drivers.KindSim {
		return referenceEnvelope{}, r, unsupported("snapshot/ref input is available on simulators only")
	}
	if mutation {
		r.Delivery = "unknown"
	}
	res := d.exec.Run(ctx, d.binary(), targetArgs(t, append(args, "--agent", "--view", "full")...)...)
	r.Raw = json.RawMessage(res.Stdout)
	var env referenceEnvelope
	if err := json.Unmarshal([]byte(res.Stdout), &env); err != nil || env.Schema != "axe.output.agent-result" || env.SchemaVersion != "1" || env.DidError == nil {
		return env, r, fmt.Errorf("axe_invalid_agent_result: unreadable snapshot/ref response")
	}
	r.Receipt = env.Data.Action
	if a := env.Data.Action; a != nil {
		var doc struct {
			Data struct {
				Action map[string]json.RawMessage `json:"action"`
			} `json:"data"`
		}
		_ = json.Unmarshal([]byte(res.Stdout), &doc)
		for _, key := range []string{"kind", "outcome", "cleanup", "enqueuedEventCount", "replayed"} {
			if len(doc.Data.Action[key]) == 0 || string(doc.Data.Action[key]) == "null" {
				return env, r, fmt.Errorf("axe_invalid_agent_receipt: missing %s", key)
			}
		}
		var replayed bool
		if json.Unmarshal(doc.Data.Action["replayed"], &replayed) != nil || replayed || a.EnqueuedEventCount < 0 {
			return env, r, fmt.Errorf("axe_invalid_agent_receipt")
		}
		switch {
		case (a.Cleanup != "released" && a.Cleanup != "notNeeded") || nonNull(a.Quarantine):
			r.Delivery = "unknown"
		case a.Outcome == "enqueuedAndDrained" && (a.Cleanup == "released" || a.Cleanup == "notNeeded"):
			r.Delivery = "completed"
		case a.EnqueuedEventCount > 0:
			r.Delivery = "partial"
		case a.Outcome == "rejectedBeforeInput" || a.Outcome == "failedBeforeInput" || a.Outcome == "busy":
			r.Delivery = "not_sent"
		}
	}
	if *env.DidError {
		code, message := "axe_agent_failed", "snapshot/ref operation failed"
		if env.Error != nil {
			code, message = env.Error.Code, env.Error.Message
		}
		if env.Data.Action == nil {
			// Source-defined host admission failures occur before native input.
			switch code {
			case "screenChanged", "targetChanged", "batchLayoutChanged", "nativeIdentityIncomplete", "unsupportedReference", "targetMissing", "snapshotExpired", "snapshotMissing":
				r.Delivery = "not_sent"
				r.Stale = true
			}
		}
		// Other refusals require a clean receipt before refreshing/replanning.
		r.Stale = r.Stale || r.Delivery == "not_sent" && env.Data.Cache.RequiresRefresh
		return env, r, fmt.Errorf("%s: %s", code, message)
	}
	if res.Err != nil || res.Code != 0 {
		return env, r, fmt.Errorf("axe_agent_process_failed")
	}
	if mutation && r.Delivery != "completed" {
		return env, r, fmt.Errorf("axe_agent_input_incomplete")
	}
	return env, r, nil
}

func (d *Driver) ReferenceSnapshot(ctx context.Context, t drivers.Target) (drivers.ReferenceSnapshot, error) {
	var snapshot drivers.ReferenceSnapshot
	args := []string{"describe-ui", "--row-limit", "64"}
	for page := 0; page < 64; page++ {
		env, _, err := d.referenceRun(ctx, t, false, args...)
		if err != nil {
			return snapshot, err
		}
		c := env.Data.Capture
		if c == nil || c.SnapshotID == "" || c.ApplicationPID <= 0 || len(c.Context) == 0 || string(c.Context) == "null" {
			return snapshot, fmt.Errorf("axe_invalid_agent_capture: identity missing")
		}
		if page == 0 {
			snapshot.ID, snapshot.PID, snapshot.Context = c.SnapshotID, c.ApplicationPID, c.Context
		} else if snapshot.ID != c.SnapshotID || snapshot.PID != c.ApplicationPID || string(snapshot.Context) != string(c.Context) {
			return snapshot, fmt.Errorf("axe_agent_snapshot_changed_during_paging")
		}
		for _, rows := range [][]string{c.Targets, c.Scroll} {
			for _, row := range rows {
				fields := strings.Split(row, "|")
				if len(fields) != 6 {
					return snapshot, fmt.Errorf("axe_invalid_agent_row")
				}
				if fields[1] == "unavailable" || c.UnavailableReferences[fields[0]] != "" {
					continue
				}
				snapshot.Elements = append(snapshot.Elements, drivers.ReferenceElement{Ref: fields[0], Action: fields[1], Role: fields[2], Label: fields[3], Value: fields[4], ID: fields[5]})
			}
		}
		if c.NextRowOffset == nil {
			return snapshot, nil
		}
		args = []string{"describe-ui", "--snapshot-id", snapshot.ID, "--row-offset", strconv.Itoa(*c.NextRowOffset), "--row-limit", "64"}
	}
	return snapshot, fmt.Errorf("axe_agent_snapshot_exceeds_4096_rows")
}

func (d *Driver) ExecuteReference(ctx context.Context, t drivers.Target, s drivers.ReferenceSnapshot, op drivers.ReferenceOperation) (drivers.ReferenceResult, error) {
	notSent := drivers.ReferenceResult{Delivery: "not_sent"}
	if s.ID == "" || s.PID <= 0 || op.Ref == "" {
		return notSent, invalid("snapshot, PID and ref required")
	}
	args := []string{}
	switch op.Kind {
	case "tap":
		args = []string{"tap"}
	case "type":
		if len(op.Text) == 0 || len(op.Text) > 256 {
			return notSent, invalid("reference typing requires 1..256 UTF-8 bytes")
		}
		args = []string{"type", op.Text} // no focus+type compound input
	case "scroll":
		if op.Direction != "up" && op.Direction != "down" {
			return notSent, invalid("invalid scroll direction")
		}
		args = []string{"swipe", "--direction", op.Direction, "--distance", "1", "--duration", "0.3"}
	default:
		return notSent, unsupported("unsupported reference operation")
	}
	args = append(args, "--snapshot-id", s.ID, "--ref", op.Ref, "--expected-pid", strconv.Itoa(s.PID))
	_, result, err := d.referenceRun(ctx, t, true, args...)
	return result, err
}
