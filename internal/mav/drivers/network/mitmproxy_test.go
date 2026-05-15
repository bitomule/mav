package network

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bitomule/mav/internal/mav/drivers"
)

// fakeExec captures Run / Start invocations and returns canned responses
// per key. Mirrors the pattern used by drivers/baguette tests.
type fakeExec struct {
	tools     map[string]bool
	runs      []string
	starts    []string
	startPID  int
	runErr    error
	startErr  error
	responses map[string]drivers.ExecResult
}

func (f *fakeExec) LookPath(name string) (string, error) {
	if f.tools[name] {
		return "/usr/local/bin/" + name, nil
	}
	return "", fmt.Errorf("not on PATH")
}

func (f *fakeExec) Run(_ context.Context, name string, args ...string) drivers.ExecResult {
	key := name
	for _, a := range args {
		key += " " + a
	}
	f.runs = append(f.runs, key)
	if r, ok := f.responses[key]; ok {
		return r
	}
	if f.runErr != nil {
		return drivers.ExecResult{Err: f.runErr}
	}
	return drivers.ExecResult{}
}

func (f *fakeExec) Start(_ context.Context, _ string, name string, args ...string) (int, error) {
	key := name
	for _, a := range args {
		key += " " + a
	}
	f.starts = append(f.starts, key)
	if f.startErr != nil {
		return 0, f.startErr
	}
	if f.startPID == 0 {
		return 12345, nil
	}
	return f.startPID, nil
}

func sim() drivers.Target { return drivers.Target{Kind: drivers.KindSim, UDID: "ABCD"} }
func dev() drivers.Target { return drivers.Target{Kind: drivers.KindDevice, UDID: "ABCD"} }

func TestProvidesEmptyOnDevice(t *testing.T) {
	d := New(&fakeExec{})
	if len(d.Provides(dev())) != 0 {
		t.Fatalf("expected empty caps on device")
	}
}

func TestProvidesNetworkCaptureOnSim(t *testing.T) {
	d := New(&fakeExec{})
	caps := d.Provides(sim())
	if !caps.Has(drivers.CapNetworkCapture) {
		t.Fatalf("expected CapNetworkCapture on sim")
	}
}

func TestProbeMissing(t *testing.T) {
	exec := &fakeExec{}
	d := New(exec)
	if got := d.Probe(context.Background(), exec); got.State != drivers.HealthMissing {
		t.Fatalf("expected Missing, got %s", got.State)
	}
}

func TestProbeOK(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"mitmdump": true}}
	d := New(exec)
	if got := d.Probe(context.Background(), exec); got.State != drivers.HealthOK {
		t.Fatalf("expected OK, got %s (%s)", got.State, got.Detail)
	}
}

func TestNetworkStartBuildsArgs(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"mitmdump": true}, startPID: 42}
	d := New(exec)
	got, err := d.NetworkStart(context.Background(), sim(), drivers.NetworkCaptureSpec{
		OutPath:    "/tmp/net.har",
		ListenPort: 9090,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 42 || got.OutPath != "/tmp/net.har" || got.ListenPort != 9090 {
		t.Fatalf("unexpected result %+v", got)
	}
	if len(exec.starts) != 1 {
		t.Fatalf("expected one start call, got %d", len(exec.starts))
	}
	cmd := exec.starts[0]
	for _, want := range []string{"mitmdump", "--listen-port 9090", "--quiet", "--set hardump=/tmp/net.har"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("expected %q in command, got %q", want, cmd)
		}
	}
}

func TestNetworkStartRequiresOutPath(t *testing.T) {
	d := New(&fakeExec{tools: map[string]bool{"mitmdump": true}})
	_, err := d.NetworkStart(context.Background(), sim(), drivers.NetworkCaptureSpec{})
	if err == nil || !strings.Contains(err.Error(), "OutPath required") {
		t.Fatalf("expected OutPath required error, got %v", err)
	}
}

func TestNetworkStartPicksFreePortWhenZero(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"mitmdump": true}, startPID: 99}
	d := New(exec)
	got, err := d.NetworkStart(context.Background(), sim(), drivers.NetworkCaptureSpec{OutPath: "/tmp/x.har"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenPort <= 0 {
		t.Fatalf("expected non-zero port, got %d", got.ListenPort)
	}
	if !strings.Contains(got.ProxyURL, fmt.Sprintf(":%d", got.ListenPort)) {
		t.Fatalf("ProxyURL=%q should contain :%d", got.ProxyURL, got.ListenPort)
	}
}

func TestNetworkStopSendsSIGTERM(t *testing.T) {
	exec := &fakeExec{tools: map[string]bool{"mitmdump": true}}
	d := New(exec)
	if err := d.NetworkStop(context.Background(), 1234); err != nil {
		t.Fatal(err)
	}
	if len(exec.runs) != 1 || exec.runs[0] != "kill -TERM 1234" {
		t.Fatalf("expected `kill -TERM 1234`, got %v", exec.runs)
	}
}

func TestNetworkStopRejectsBadPID(t *testing.T) {
	d := New(&fakeExec{tools: map[string]bool{"mitmdump": true}})
	if err := d.NetworkStop(context.Background(), 0); err == nil {
		t.Fatal("expected error on pid=0")
	}
}
