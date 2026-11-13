package macos

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// CUA tokens are snapshot-local, not AXIdentifiers. Keep them out of the
// semantic tree so a token refresh cannot masquerade as a screen transition.
func (d *Cua) ReferenceTree(ctx context.Context, t drivers.Target) (drivers.TreeResult, drivers.ReferenceSnapshot, error) {
	state, err := d.windowState(ctx, t)
	if err != nil {
		return drivers.TreeResult{}, drivers.ReferenceSnapshot{}, err
	}
	s := drivers.ReferenceSnapshot{ID: state.SnapshotID, PID: state.PID}
	s.Context, _ = json.Marshal(map[string]int{"pid": state.PID})
	nodes := cuaElementsToNodes(state.Elements)
	for i, el := range state.Elements {
		delete(nodes[i], "identifier")
		frame, _ := nodes[i]["frame"].(string)
		action := "tap"
		if strings.Contains(strings.ToLower(el.Role), "scroll") {
			action = "swipe"
		}
		s.Elements = append(s.Elements, drivers.ReferenceElement{Ref: el.Token, Action: action, Role: el.Role, Label: el.Label, Value: el.Value, Frame: frame})
	}
	b, err := json.Marshal(nodes)
	return drivers.TreeResult{JSON: b}, s, err
}

func (d *Cua) ReferenceSnapshot(ctx context.Context, t drivers.Target) (drivers.ReferenceSnapshot, error) {
	_, s, err := d.ReferenceTree(ctx, t)
	return s, err
}

func (d *Cua) ExecuteReference(ctx context.Context, t drivers.Target, s drivers.ReferenceSnapshot, op drivers.ReferenceOperation) (drivers.ReferenceResult, error) {
	r := drivers.ReferenceResult{Delivery: "not_sent"}
	if t.Kind != drivers.KindMac || s.ID == "" || s.PID <= 0 || op.Ref == "" || t.PID != 0 && t.PID != s.PID {
		return r, fmt.Errorf("cua: reference context missing or changed")
	}
	found := false
	for _, el := range s.Elements {
		if el.Ref == op.Ref {
			found = true
			break
		}
	}
	if !found {
		return r, fmt.Errorf("cua: reference not in observation")
	}
	args := map[string]any{"pid": s.PID, "element_token": op.Ref}
	tool := ""
	switch op.Kind {
	case "tap":
		tool = "click"
	case "scroll":
		tool = "scroll"
		direction := map[string]string{"up": "down", "down": "up"}[op.Direction]
		if direction == "" {
			return r, fmt.Errorf("cua: invalid scroll direction")
		}
		args["direction"] = direction
		args["delivery_mode"] = "background"
	default:
		return r, fmt.Errorf("cua: goal operation unavailable with checked references")
	}
	// No new get_window_state here: that would invalidate op.Ref. The broker
	// owns admission of the fresh token. A lost response has unknown delivery.
	r.Delivery = "unknown"
	payload, _ := json.Marshal(args)
	res := d.exec.Run(ctx, "cua-driver", "call", tool, string(payload))
	raw := []byte(res.Stdout)
	if json.Valid(raw) {
		r.Raw = raw
	}
	if res.Err != nil || res.Code != 0 || !json.Valid(raw) || string(raw) == "null" {
		return r, fmt.Errorf("cua: checked input response unavailable")
	}
	var envelope struct {
		Refusal json.RawMessage `json:"refusal"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Refusal) > 0 && string(envelope.Refusal) != "null" {
		return r, fmt.Errorf("cua: checked input refused")
	}
	r.Delivery = "completed"
	return r, nil
}
