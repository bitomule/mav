package mav

import (
	"strings"
	"testing"
)

// fixture: a minimal but realistic iOS .ips body. Real reports have many
// more fields; the parser tolerates them via "ignore unknown keys".
const sampleIPS = `{"app_name":"Boxy","timestamp":"2026-05-15 21:00:00.00 +0200","app_version":"1.2.3","slice_uuid":"abc","build_version":"123","platform":2,"bundleID":"com.example.boxy","share_with_app_devs":1,"is_first_party":0,"bug_type":"309","os_version":"iPhone OS 26.2 (23A123)","incident_id":"INC-1"}
{
  "uptime": 12345,
  "procName": "Boxy",
  "procPath": "/private/var/containers/Bundle/Application/.../Boxy.app/Boxy",
  "exception": {"codes":"0x0000000000000001, 0x0000000000000000","rawCodes":[1,0],"type":"EXC_BAD_ACCESS","signal":"SIGSEGV","subtype":"KERN_INVALID_ADDRESS at 0x000000010cdef000"},
  "termination": {"flags":0,"code":11,"namespace":"SIGNAL","indicator":"Segmentation fault: 11"},
  "asi": {"Boxy": ["NSRangeException: index 99 beyond bounds [0 .. 3]"]},
  "threads": [
    {"name":"main thread","queue":"com.apple.main-thread","triggered":true},
    {"name":"worker","queue":"com.example.worker","triggered":false}
  ]
}`

func TestParseIPSExtractsHeaderAndBody(t *testing.T) {
	got, err := ParseIPS([]byte(sampleIPS))
	if err != nil {
		t.Fatal(err)
	}
	if got.BundleID != "com.example.boxy" {
		t.Errorf("BundleID=%q", got.BundleID)
	}
	if got.AppName != "Boxy" {
		t.Errorf("AppName=%q", got.AppName)
	}
	if got.OSVersion != "iPhone OS 26.2 (23A123)" {
		t.Errorf("OSVersion=%q", got.OSVersion)
	}
	if got.IncidentID != "INC-1" {
		t.Errorf("IncidentID=%q", got.IncidentID)
	}
	if got.Process != "Boxy" {
		t.Errorf("Process=%q", got.Process)
	}
	if got.Exception != "EXC_BAD_ACCESS (SIGSEGV)" {
		t.Errorf("Exception=%q", got.Exception)
	}
	if got.Termination != "SIGNAL code=11 (Segmentation fault: 11)" {
		t.Errorf("Termination=%q", got.Termination)
	}
	if got.Reason != "KERN_INVALID_ADDRESS at 0x000000010cdef000" {
		t.Errorf("Reason=%q", got.Reason)
	}
	if got.CrashedThread != "main thread" {
		t.Errorf("CrashedThread=%q", got.CrashedThread)
	}
	if got.BugType != "309" {
		t.Errorf("BugType=%q", got.BugType)
	}
}

func TestParseIPSFallsBackToASIWhenSubtypeMissing(t *testing.T) {
	ips := `{"app_name":"X","bundleID":"com.x","bug_type":"309","os_version":"iOS 26"}
{"procName":"X","exception":{"type":"NSInvalidArgumentException"},"asi":{"X":["NSInvalidArgumentException: -[NSNull length] unrecognized selector"]}}`
	got, err := ParseIPS([]byte(ips))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Reason, "unrecognized selector") {
		t.Errorf("expected ASI-derived reason, got %q", got.Reason)
	}
}

func TestParseIPSMissingBodySeparator(t *testing.T) {
	_, err := ParseIPS([]byte(`{"app_name":"X"}`))
	if err == nil {
		t.Fatal("expected error on missing body")
	}
}

func TestParseIPSMalformedHeader(t *testing.T) {
	_, err := ParseIPS([]byte("not json\n{}"))
	if err == nil {
		t.Fatal("expected error on malformed header")
	}
}

func TestParseIPSBestEffortBody(t *testing.T) {
	// Header valid, body garbage: the parser should still return the
	// header fields rather than fail outright. This matters because some
	// .ips files MAV will encounter come from older iOS bug types and
	// may have body shapes we don't recognise.
	ips := `{"app_name":"X","bundleID":"com.x","bug_type":"100","timestamp":"t","os_version":"iOS 26"}
{ this isn't valid json }`
	got, err := ParseIPS([]byte(ips))
	if err != nil {
		t.Fatalf("expected best-effort success, got error: %v", err)
	}
	if got.BundleID != "com.x" {
		t.Errorf("BundleID=%q", got.BundleID)
	}
	if got.Exception != "" {
		t.Errorf("expected empty exception when body unparseable, got %q", got.Exception)
	}
}

func TestCrashSummaryOneLiner(t *testing.T) {
	s := CrashSummary{
		Exception: "EXC_BAD_ACCESS (SIGSEGV)",
		Process:   "Boxy",
		BundleID:  "com.example.boxy",
		Reason:    "KERN_INVALID_ADDRESS at 0x...",
	}
	got := s.OneLiner()
	want := "EXC_BAD_ACCESS (SIGSEGV) in Boxy on com.example.boxy — KERN_INVALID_ADDRESS at 0x..."
	if got != want {
		t.Errorf("OneLiner()=%q, want %q", got, want)
	}
}

func TestCrashSummaryOneLinerWithoutReason(t *testing.T) {
	s := CrashSummary{Exception: "EXC_CRASH", Process: "X", BundleID: "com.x"}
	got := s.OneLiner()
	want := "EXC_CRASH in X on com.x"
	if got != want {
		t.Errorf("OneLiner()=%q, want %q", got, want)
	}
}
