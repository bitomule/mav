package mav

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// CrashSummary is the parsed view of an iOS .ips crash report. iOS 15+
// stores .ips as two concatenated JSON documents: a header line followed
// by a body. We extract just the fields agents and humans actually need
// for triage; the full body stays in the original .ips on disk.
type CrashSummary struct {
	// Header fields
	BundleID   string `json:"bundle_id,omitempty"`
	AppName    string `json:"app_name,omitempty"`
	AppVersion string `json:"app_version,omitempty"`
	OSVersion  string `json:"os_version,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	IncidentID string `json:"incident_id,omitempty"`
	BugType    string `json:"bug_type,omitempty"`

	// Body fields (best-effort, may be empty depending on bug_type)
	Process       string `json:"process,omitempty"`
	Exception     string `json:"exception,omitempty"`      // e.g., "EXC_BAD_ACCESS (SIGSEGV)"
	Termination   string `json:"termination,omitempty"`    // e.g., "SIGNAL Code -1"
	Reason        string `json:"reason,omitempty"`         // exception.subtype + asi when present
	CrashedThread string `json:"crashed_thread,omitempty"` // thread name when present
}

// ipsHeader matches the subset of the first-line JSON we care about. iOS
// emits many more keys; ignored fields are tolerated by encoding/json.
type ipsHeader struct {
	AppName    string `json:"app_name"`
	BundleID   string `json:"bundleID"`
	AppVersion string `json:"app_version"`
	OSVersion  string `json:"os_version"`
	Timestamp  string `json:"timestamp"`
	IncidentID string `json:"incident_id"`
	BugType    string `json:"bug_type"`
}

// ipsBody matches the second-document subset we care about.
type ipsBody struct {
	ProcName    string              `json:"procName"`
	Exception   ipsException        `json:"exception"`
	Termination ipsTermination      `json:"termination"`
	ASI         map[string][]string `json:"asi"`
	Threads     []ipsThread         `json:"threads"`
}

type ipsException struct {
	Type    string `json:"type"`
	Signal  string `json:"signal"`
	Subtype string `json:"subtype"`
	Codes   string `json:"codes"`
}

type ipsTermination struct {
	Namespace string `json:"namespace"`
	Code      int    `json:"code"`
	Indicator string `json:"indicator"`
}

type ipsThread struct {
	Name      string `json:"name"`
	Queue     string `json:"queue"`
	Triggered bool   `json:"triggered"`
}

// ParseIPS reads an iOS 15+ .ips body and extracts a CrashSummary.
//
// The .ips format is two JSON documents separated by a newline: the first
// line is the header (app/bundle/version metadata), the rest is the body
// (exception, termination, threads, ...). We split on the first `\n{`
// boundary so a header with arbitrary keys still parses cleanly.
//
// Older formats (iOS 14 and below; PLIST-style symbol-rich text) are NOT
// supported -- this parser intentionally targets the modern JSON format
// where idb's `crash show` output now lives.
func ParseIPS(raw []byte) (CrashSummary, error) {
	// Split header from body. The first newline followed by `{` (the body
	// opening brace) is the canonical separator.
	body := raw
	headerEnd := bytes.IndexByte(body, '\n')
	if headerEnd < 0 || headerEnd == len(body)-1 {
		return CrashSummary{}, fmt.Errorf("ips: missing body separator")
	}
	headerBytes := bytes.TrimSpace(body[:headerEnd])
	bodyBytes := bytes.TrimSpace(body[headerEnd+1:])

	if len(headerBytes) == 0 || headerBytes[0] != '{' {
		return CrashSummary{}, fmt.Errorf("ips: malformed header")
	}
	if len(bodyBytes) == 0 || bodyBytes[0] != '{' {
		return CrashSummary{}, fmt.Errorf("ips: malformed body")
	}

	var h ipsHeader
	if err := json.Unmarshal(headerBytes, &h); err != nil {
		return CrashSummary{}, fmt.Errorf("ips header: %w", err)
	}

	var b ipsBody
	// Body parse is best-effort: if it fails we still return the header
	// summary so triage can at least show the bundle/timestamp.
	_ = json.Unmarshal(bodyBytes, &b)

	summary := CrashSummary{
		AppName:    h.AppName,
		BundleID:   h.BundleID,
		AppVersion: h.AppVersion,
		OSVersion:  h.OSVersion,
		Timestamp:  h.Timestamp,
		IncidentID: h.IncidentID,
		BugType:    h.BugType,
		Process:    b.ProcName,
	}

	if b.Exception.Type != "" {
		if b.Exception.Signal != "" {
			summary.Exception = fmt.Sprintf("%s (%s)", b.Exception.Type, b.Exception.Signal)
		} else {
			summary.Exception = b.Exception.Type
		}
	}
	if b.Termination.Namespace != "" {
		summary.Termination = fmt.Sprintf("%s code=%d", b.Termination.Namespace, b.Termination.Code)
		if b.Termination.Indicator != "" {
			summary.Termination += " (" + b.Termination.Indicator + ")"
		}
	}

	// Build a short reason line. exception.subtype is usually the
	// human-readable cause ("KERN_INVALID_ADDRESS at 0x..."); fall back to
	// the asi (application-specific information) lines when missing.
	switch {
	case b.Exception.Subtype != "":
		summary.Reason = b.Exception.Subtype
	case len(b.ASI) > 0:
		// asi is map[process]string[]; flatten the first non-empty entry.
		for _, lines := range b.ASI {
			if len(lines) > 0 {
				summary.Reason = strings.TrimSpace(lines[0])
				break
			}
		}
	}

	for _, t := range b.Threads {
		if t.Triggered {
			if t.Name != "" {
				summary.CrashedThread = t.Name
			} else if t.Queue != "" {
				summary.CrashedThread = "queue: " + t.Queue
			}
			break
		}
	}

	return summary, nil
}

// OneLiner renders a single-line summary suitable for the HTML report's
// crash card. Format: "EXC_BAD_ACCESS (SIGSEGV) in MyApp on com.example —
// reason". Empty when the summary lacks the bits we need.
func (s CrashSummary) OneLiner() string {
	parts := []string{}
	if s.Exception != "" {
		parts = append(parts, s.Exception)
	}
	if s.Process != "" {
		parts = append(parts, "in "+s.Process)
	} else if s.AppName != "" {
		parts = append(parts, "in "+s.AppName)
	}
	if s.BundleID != "" {
		parts = append(parts, "on "+s.BundleID)
	}
	header := strings.Join(parts, " ")
	if s.Reason != "" {
		if header == "" {
			return s.Reason
		}
		return header + " — " + s.Reason
	}
	return header
}
