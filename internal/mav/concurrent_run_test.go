package mav

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This file exercises the M1 fix (killing the .mav/current-run global
// pointer as a source of truth for `mav run`) with real, separate OS
// processes racing each other -- not goroutines. The bug lived entirely in
// state shared across processes (a file on disk, unix sockets, signals sent
// by PID); a single-process/goroutine test cannot touch any of that, since
// it would just be exercising one CLI value's in-memory state, which was
// never where the corruption happened.
//
// Every test here re-execs this same test binary as a child via
// os.Executable() + TestMain's MAV_TEST_CHILD branch (see main_test.go), so
// it goes through the real ExecRunner (fork/exec, Setpgid, real PIDs) end to
// end, exactly like a real `mav` invocation would.

// mkShortRoot creates a project root directly under /tmp instead of
// t.TempDir(), which nests inside a per-test directory whose name embeds the
// full test name (e.g. .../TestConcurrentRunsKeepSeparateWorkers1234567/001)
// -- routinely 90+ characters before .mav/runs/<id>/worker.sock is even
// appended. That blows past macOS's ~104-byte sun_path limit for unix
// domain sockets, so the worker's net.Listen fails with "bind: invalid
// argument" and open silently falls back to session=direct. Any test that
// exercises a real open (and therefore a real worker socket) needs this
// short root instead.
func mkShortRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "mavcc")
	if err != nil {
		t.Fatalf("mkShortRoot: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// writeFakeXcrun creates a minimal `xcrun` shim on its own PATH directory.
// `simctl spawn <udid> log stream ...` execs into a real, long-lived `sleep`
// so probe-logs gets a real OS PID a `kill -0` can observe; every other
// invocation (boot/bootstatus/shutdown/...) is a no-op success. This keeps
// the tests hermetic -- no real simulator involved -- while still exercising
// the real fork/exec/Setpgid path startProbeLogs relies on.
func writeFakeXcrun(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"prev=\"\"\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"log\" ] && [ \"$arg\" = \"stream\" ]; then\n" +
		"    exec sleep 300\n" +
		"  fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
		"exit 0\n"
	path := filepath.Join(dir, "xcrun")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeConcurrencyConfig writes a .mav/config.yaml whose entire launch
// recipe is shell no-ops (a bare "true", plus an "echo" that satisfies
// app_path's ".app" parsing), so `open` never depends on a real build --
// only on the fake xcrun above for boot/bootstatus and the probe-logs log
// stream. allow_shell is on so the barrier flows below can use exec steps.
func writeConcurrencyConfig(t *testing.T, root string) {
	t.Helper()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.SimulatorName = "iPhone"
	cfg.AllowShell = true
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Build:   "true",
		AppPath: "echo /tmp/App.app",
		Install: "true",
		Launch:  "true",
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
}

// startChild re-execs this same test binary as a real child OS process
// running `mav run <flowPath>`. It only starts the process; the caller owns
// waiting so two children can be running concurrently.
func startChild(t *testing.T, ctx context.Context, root, flowPath, binDir, tmpDir string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := exec.CommandContext(ctx, self, "run", flowPath)
	cmd.Dir = root
	cmd.Env = []string{
		"MAV_TEST_CHILD=1",
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + tmpDir,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	return cmd, &stdout, &stderr
}

func waitForMarker(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for marker %s", path)
}

func readMarker(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read marker %s: %v", path, err)
	}
	return strings.TrimSpace(string(data))
}

func touchMarker(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// extractField pulls "name=value" out of a line of mav's key=value stdout
// (see Output.Write in output.go); values here are never quoted (run ids
// are plain hex), so a simple word-boundary match is enough.
func extractField(t *testing.T, output, name string) string {
	t.Helper()
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `=(\S+)`)
	m := re.FindStringSubmatch(output)
	if m == nil {
		t.Fatalf("output missing %s=: %s", name, output)
	}
	return m[1]
}

// killRunProcesses kills every PID recorded across every run under root, so
// a failed assertion (or a timed-out child) doesn't leak probe-logs "sleep"
// processes or worker daemons past the test.
func killRunProcesses(root string) {
	runsDir := filepath.Join(root, MavDir, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		run := RunState{
			ID:        entry.Name(),
			Dir:       filepath.Join(runsDir, entry.Name()),
			Processes: filepath.Join(runsDir, entry.Name(), "processes.jsonl"),
		}
		for _, record := range loadProcessRecords(run) {
			if record.PID > 0 {
				_ = stopProcess(record.PID)
			}
		}
	}
}

type coordPaths struct {
	aRunID    string
	aOpened   string
	bRunID    string
	bOpened   string
	checkDone string
}

func newCoordPaths(t *testing.T) coordPaths {
	dir := t.TempDir()
	return coordPaths{
		aRunID:    filepath.Join(dir, "a_run_id"),
		aOpened:   filepath.Join(dir, "a_opened"),
		bRunID:    filepath.Join(dir, "b_run_id"),
		bOpened:   filepath.Join(dir, "b_opened"),
		checkDone: filepath.Join(dir, "check_done"),
	}
}

// writeConcurrentFlows writes two native MAV flows designed to force a
// specific interleaving between two independent processes sharing one repo
// root, using exec-step file barriers (exec steps already export
// MAV_RUN_ID/MAV_RUN_DIR, see execFlowShellOutput):
//
//  1. agent-a opens, then signals a_opened and blocks.
//  2. agent-b waits for a_opened, only then opens, then signals b_opened.
//  3. Both then block on check_done so the test can inspect live state
//     (recorded PIDs, worker sockets) before either self-stops.
//
// Without the barrier this would be flaky and would prove nothing: the
// interleaving is the entire point of the test.
func writeConcurrentFlows(t *testing.T, root string, coord coordPaths) (flowA, flowB string) {
	t.Helper()
	flowA = filepath.Join(root, "agent-a.yaml")
	flowB = filepath.Join(root, "agent-b.yaml")

	contentA := fmt.Sprintf(`name: agent-a
steps:
  - open: {}
  - exec:
      cmd: 'echo "$MAV_RUN_ID" > "%s"; touch "%s"'
      timeout: 15s
  - exec:
      cmd: 'until [ -f "%s" ]; do sleep 0.05; done'
      timeout: 20s
  - exec:
      cmd: 'until [ -f "%s" ]; do sleep 0.05; done'
      timeout: 20s
`, coord.aRunID, coord.aOpened, coord.bOpened, coord.checkDone)

	contentB := fmt.Sprintf(`name: agent-b
steps:
  - exec:
      cmd: 'until [ -f "%s" ]; do sleep 0.05; done'
      timeout: 20s
  - open: {}
  - exec:
      cmd: 'echo "$MAV_RUN_ID" > "%s"; touch "%s"'
      timeout: 15s
  - exec:
      cmd: 'until [ -f "%s" ]; do sleep 0.05; done'
      timeout: 20s
`, coord.aOpened, coord.bRunID, coord.bOpened, coord.checkDone)

	if err := os.WriteFile(flowA, []byte(contentA), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flowB, []byte(contentB), 0o644); err != nil {
		t.Fatal(err)
	}
	return flowA, flowB
}

type concurrentHarness struct {
	root       string
	coord      coordPaths
	cmdA, cmdB *exec.Cmd
	outA, outB *bytes.Buffer
	errA, errB *bytes.Buffer
}

func startConcurrentAgents(t *testing.T) *concurrentHarness {
	t.Helper()
	root := mkShortRoot(t)
	writeConcurrencyConfig(t, root)
	binDir := writeFakeXcrun(t)
	coord := newCoordPaths(t)
	flowA, flowB := writeConcurrentFlows(t, root, coord)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	h := &concurrentHarness{root: root, coord: coord}
	h.cmdA, h.outA, h.errA = startChild(t, ctx, root, flowA, binDir, t.TempDir())
	h.cmdB, h.outB, h.errB = startChild(t, ctx, root, flowB, binDir, t.TempDir())
	t.Cleanup(func() {
		cancel()
		if h.cmdA.Process != nil {
			_ = h.cmdA.Process.Kill()
		}
		if h.cmdB.Process != nil {
			_ = h.cmdB.Process.Kill()
		}
		killRunProcesses(root)
	})
	return h
}

// TestConcurrentRunsDoNotAdoptEachOthersRun is the regression test for the
// M1 bug: two `mav run` invocations against the same repo used to corrupt
// each other silently through .mav/current-run. Before the fix, agent B's
// open would read the pointer (agent A's run, since A opened first), run
// `stop --run` on it -- killing A's probe-logs process out from under it --
// and overwrite the pointer with its own run. Assertions still passed
// because it's the same app: a green false positive.
func TestConcurrentRunsDoNotAdoptEachOthersRun(t *testing.T) {
	h := startConcurrentAgents(t)

	waitForMarker(t, h.coord.aOpened, 15*time.Second)
	waitForMarker(t, h.coord.bOpened, 15*time.Second)

	runAID := readMarker(t, h.coord.aRunID)
	runBID := readMarker(t, h.coord.bRunID)
	if runAID == "" || runBID == "" {
		t.Fatalf("empty run ids: a=%q b=%q", runAID, runBID)
	}
	if runAID == runBID {
		t.Fatalf("agent A and agent B adopted the same run: %s", runAID)
	}

	runA, err := LoadRun(h.root, runAID)
	if err != nil {
		t.Fatalf("LoadRun A: %v", err)
	}
	runB, err := LoadRun(h.root, runBID)
	if err != nil {
		t.Fatalf("LoadRun B: %v", err)
	}
	if runA.Dir == runB.Dir {
		t.Fatalf("agent A and B share a run directory: %s", runA.Dir)
	}

	probeLogsPID := 0
	for _, record := range loadProcessRecords(runA) {
		if record.Kind == "probe-logs" {
			probeLogsPID = record.PID
		}
	}
	if probeLogsPID <= 0 {
		t.Fatalf("agent A has no recorded probe-logs pid: %+v", loadProcessRecords(runA))
	}
	if !processAlive(probeLogsPID) {
		t.Fatalf("agent A's probe-logs process (pid %d) is dead: agent B's open killed agent A's run", probeLogsPID)
	}

	touchMarker(t, h.coord.checkDone)
	if err := h.cmdA.Wait(); err != nil {
		t.Fatalf("agent A failed: %v\nstdout=%s\nstderr=%s", err, h.outA, h.errA)
	}
	if err := h.cmdB.Wait(); err != nil {
		t.Fatalf("agent B failed: %v\nstdout=%s\nstderr=%s", err, h.outB, h.errB)
	}

	if !strings.Contains(h.outA.String(), "run="+runAID) {
		t.Fatalf("agent A stdout run id mismatch: %s", h.outA)
	}
	if !strings.Contains(h.outB.String(), "run="+runBID) {
		t.Fatalf("agent B stdout run id mismatch: %s", h.outB)
	}

	for _, run := range []RunState{runA, runB} {
		data, err := os.ReadFile(filepath.Join(run.Dir, "run.json"))
		if err != nil {
			t.Fatalf("read run.json for %s: %v", run.ID, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("parse run.json for %s: %v", run.ID, err)
		}
		if parsed["status"] != "passed" {
			t.Fatalf("run %s status=%v, want passed: %s", run.ID, parsed["status"], data)
		}
	}
}

// TestConcurrentRunsKeepSeparateWorkers proves the two agents never converge
// on a single worker. Under the old bug both would end up bound to the same
// run (whichever .mav/current-run pointed to at the moment each read it),
// and startRunWorker's workerPing-first check means the second agent would
// silently reuse the first's worker instead of spawning its own -- the root
// cause of gestures from one agent landing on the other's simulator
// (sendWorkerGestureWithRestart, cli.go).
func TestConcurrentRunsKeepSeparateWorkers(t *testing.T) {
	h := startConcurrentAgents(t)

	waitForMarker(t, h.coord.aOpened, 15*time.Second)
	waitForMarker(t, h.coord.bOpened, 15*time.Second)

	runAID := readMarker(t, h.coord.aRunID)
	runBID := readMarker(t, h.coord.bRunID)
	runA, err := LoadRun(h.root, runAID)
	if err != nil {
		t.Fatalf("LoadRun A: %v", err)
	}
	runB, err := LoadRun(h.root, runBID)
	if err != nil {
		t.Fatalf("LoadRun B: %v", err)
	}

	if workerSocket(runA) == workerSocket(runB) {
		t.Fatalf("agents share a worker socket: %s", workerSocket(runA))
	}
	if !workerPing(runA) {
		log, _ := os.ReadFile(filepath.Join(runA.Dir, "worker.log"))
		t.Fatalf("agent A's worker did not respond on %s\nworker.log=%s\nstdout=%s", workerSocket(runA), log, h.outA)
	}
	if !workerPing(runB) {
		log, _ := os.ReadFile(filepath.Join(runB.Dir, "worker.log"))
		t.Fatalf("agent B's worker did not respond on %s\nworker.log=%s\nstdout=%s", workerSocket(runB), log, h.outB)
	}

	touchMarker(t, h.coord.checkDone)
	if err := h.cmdA.Wait(); err != nil {
		t.Fatalf("agent A failed: %v\nstdout=%s\nstderr=%s", err, h.outA, h.errA)
	}
	if err := h.cmdB.Wait(); err != nil {
		t.Fatalf("agent B failed: %v\nstdout=%s\nstderr=%s", err, h.outB, h.errB)
	}
}

// TestManualOpenPreservesCurrentRunSemantics is the compatibility canary: a
// standalone `mav open` (never through a flow, so no run is ever bound) must
// keep its pre-M1 behavior exactly -- read .mav/current-run, kill whatever
// it names, overwrite it with the freshly opened run.
func TestManualOpenPreservesCurrentRunSemantics(t *testing.T) {
	root := mkShortRoot(t)
	writeConcurrencyConfig(t, root)
	binDir := writeFakeXcrun(t)
	tmp := t.TempDir()
	t.Cleanup(func() { killRunProcesses(root) })

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	runOpen := func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, self, "open")
		cmd.Dir = root
		cmd.Env = []string{
			"MAV_TEST_CHILD=1",
			"PATH=" + binDir + ":" + os.Getenv("PATH"),
			"HOME=" + os.Getenv("HOME"),
			"TMPDIR=" + tmp,
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("mav open failed: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	firstOut := runOpen()
	firstID := extractField(t, firstOut, "run")
	firstRun, err := LoadRun(root, "")
	if err != nil {
		t.Fatalf("LoadRun after first open: %v", err)
	}
	if firstRun.ID != firstID {
		t.Fatalf(".mav/current-run=%s, want %s (first open)", firstRun.ID, firstID)
	}
	firstProbePID := 0
	for _, record := range loadProcessRecords(firstRun) {
		if record.Kind == "probe-logs" {
			firstProbePID = record.PID
		}
	}
	if firstProbePID <= 0 {
		t.Fatalf("first open recorded no probe-logs pid")
	}
	if !processAlive(firstProbePID) {
		t.Fatalf("first open's probe-logs process is already dead")
	}

	secondOut := runOpen()
	secondID := extractField(t, secondOut, "run")
	if secondID == firstID {
		t.Fatalf("second manual open reused the first run: %s", secondID)
	}
	secondRun, err := LoadRun(root, "")
	if err != nil {
		t.Fatalf("LoadRun after second open: %v", err)
	}
	if secondRun.ID != secondID {
		t.Fatalf(".mav/current-run=%s, want %s (second open)", secondRun.ID, secondID)
	}

	deadline := time.Now().Add(5 * time.Second)
	for processAlive(firstProbePID) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(firstProbePID) {
		t.Fatalf("second manual open did not stop the first open's probe-logs process (pid %d)", firstProbePID)
	}
}

// TestFlowRunPublishesCurrentRunOnCompletion protects the compatibility
// contract runFlow keeps despite never reading .mav/current-run anymore: it
// still writes the pointer once the flow is fully done, so manual follow-up
// commands (mav logs / evidence report / crashes without --run) keep
// working after `mav run` finishes.
func TestFlowRunPublishesCurrentRunOnCompletion(t *testing.T) {
	root := t.TempDir()
	writeConcurrencyConfig(t, root)
	binDir := writeFakeXcrun(t)
	t.Cleanup(func() { killRunProcesses(root) })

	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("name: trivial\nsteps:\n  - exec: { cmd: 'echo hello' }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd, out, errBuf := startChild(t, ctx, root, flowPath, binDir, t.TempDir())
	if err := cmd.Wait(); err != nil {
		t.Fatalf("mav run failed: %v\nstdout=%s\nstderr=%s", err, out, errBuf)
	}
	runID := extractField(t, out.String(), "run")
	run, err := LoadRun(root, "")
	if err != nil {
		t.Fatalf("LoadRun after mav run: %v", err)
	}
	if run.ID != runID {
		t.Fatalf(".mav/current-run=%s, want %s", run.ID, runID)
	}
}

// TestFlowOpenIgnoresStaleCurrentRun is the cheap in-process unit: a stale
// .mav/current-run pointing at a run whose recorded probe-logs process is
// still alive must not be read, adopted, or killed by a flow's open step.
// Unlike the process-pair tests above this needs no real xcrun/probe-logs
// simulation -- fakeRunner is enough, since the only thing under test is
// whether runFlow's open step ever calls LoadRun(root, "") at all.
//
// processAlive is kill(pid, 0) (simlock.go), which reports true for a zombie
// -- a process that already died but hasn't been reaped -- because the PID
// stays valid in the process table until something calls wait() on it. The
// stale "sleep 30" here is started directly by this test process, and
// nothing reaps it until t.Cleanup, so a naive processAlive(stalePID) check
// after the flow runs would read true whether or not the flow actually
// killed it: not a passing test, a non-test. To make that assertion mean
// something, a background goroutine calls Wait() on the child itself,
// racing the flow to reap it; only a real kill makes that goroutine finish
// before the flow does. Setpgid mirrors how startProbeLogs actually spawns
// probe-logs (own process group), and lets cleanup kill the whole group
// instead of guessing whether Wait() already reaped it.
func TestFlowOpenIgnoresStaleCurrentRun(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.SimulatorName = "iPhone"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Build:   "true",
		AppPath: "echo /tmp/App.app",
		Install: "true",
		Launch:  "true",
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	sleep := exec.Command("sleep", "30")
	sleep.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := sleep.Start(); err != nil {
		t.Fatalf("start stale sleep: %v", err)
	}
	stalePID := sleep.Process.Pid
	reaped := make(chan struct{})
	go func() {
		_ = sleep.Wait()
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-stalePID, syscall.SIGKILL)
		<-reaped
	})

	bogus, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	appendProcess(bogus, "probe-logs", stalePID, "sleep 30")
	if err := SaveCurrentRun(root, bogus); err != nil {
		t.Fatal(err)
	}

	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("steps:\n  - open: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"echo /tmp/App.app": {Stdout: "/tmp/App.app\n"}},
	}
	var out bytes.Buffer
	cli := CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}
	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok cmd=run") {
		t.Fatalf("flow did not succeed: %s", out.String())
	}
	runID := extractField(t, out.String(), "run")
	if runID == bogus.ID {
		t.Fatalf("flow adopted the stale current-run: %s", bogus.ID)
	}

	select {
	case <-reaped:
		t.Fatalf("flow's open killed the stale run's registered process (pid %d): it must never read or touch .mav/current-run", stalePID)
	default:
	}
	if !processAlive(stalePID) {
		t.Fatalf("flow's open killed the stale run's registered process (pid %d): it must never read or touch .mav/current-run", stalePID)
	}

	// Discriminates against the exact pre-M1 bug: main's `open` always read
	// .mav/current-run and unconditionally overwrote it with the new run's
	// id as one of its first actions, regardless of whether the run it
	// named was still alive. Post-fix, runFlow's own end-of-flow publish
	// (publishCurrentRunIfSafe) must not steal the pointer from bogus either,
	// since bogus's probe-logs process is still alive throughout.
	stillCurrent, err := LoadRun(root, "")
	if err != nil {
		t.Fatalf("LoadRun after flow: %v", err)
	}
	if stillCurrent.ID != bogus.ID {
		t.Fatalf(".mav/current-run=%s after the flow, want unchanged %s (bogus's process is still alive)", stillCurrent.ID, bogus.ID)
	}
}

func concurrencyTestCLI(t *testing.T, root string) (CLI, *bytes.Buffer) {
	t.Helper()
	cfg := DefaultConfig(root)
	cfg.BundleID = "com.example.app"
	cfg.SimulatorUDID = "SIM"
	cfg.SimulatorName = "iPhone"
	cfg.Launch = LaunchConfig{Mode: "custom", Commands: LaunchCommands{
		Build:   "true",
		AppPath: "echo /tmp/App.app",
		Install: "true",
		Launch:  "true",
	}}
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &launchRecipeRunner{
		tools:   map[string]bool{"xcrun": true},
		results: map[string]CommandResult{"echo /tmp/App.app": {Stdout: "/tmp/App.app\n"}},
	}
	var out bytes.Buffer
	return CLI{Runner: runner, Root: root, Stdout: &out, Stderr: &bytes.Buffer{}}, &out
}

// TestFlowRunRejectsUnknownRunID is the regression test for the silent
// verde-falso reported against M1: `mav run flow.yaml --run <typo'd id>`
// used to exit 0 against a run.Dir that LoadRun handed back without ever
// stat'ing it, so the whole flow "succeeded" while writing to a directory
// nothing else would ever look at, and left .mav/current-run pointing at
// an id that names nothing on disk.
func TestFlowRunRejectsUnknownRunID(t *testing.T) {
	root := t.TempDir()
	cli, out := concurrencyTestCLI(t, root)

	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("name: t\nsteps:\n  - exec: { cmd: 'echo hi' }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AllowShell = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	// A flow failure comes out as a `fail` line AND as CommandFailed, so
	// the exit code matches what the output says. What is still not
	// acceptable is a raw Go error, which carries no code an agent can
	// read.
	var failed CommandFailed
	if err = cli.Run(context.Background(), []string{"run", flowPath, "--run", "bogus123"}); err != nil && !errors.As(err, &failed) {
		t.Fatalf("runFlow returned a Go error instead of a Fail() payload: %v", err)
	}
	if !strings.Contains(out.String(), "fail code=run_not_found") {
		t.Fatalf("expected fail code=run_not_found, got: %s", out.String())
	}
	if _, statErr := os.Stat(filepath.Join(root, MavDir, "runs", "bogus123")); statErr == nil {
		t.Fatalf("run_not_found should mean nothing was ever written for bogus123")
	}
	if _, loadErr := LoadRun(root, ""); loadErr == nil {
		t.Fatalf(".mav/current-run must stay unset when --run fails validation")
	}
}

// TestFlowRunContinuesExistingRunID is the compatibility canary for the same
// fix: a valid --run naming a run that genuinely exists on disk (the
// documented "continue evidence collection on an already-open run" use
// case) must still work exactly as before.
func TestFlowRunContinuesExistingRunID(t *testing.T) {
	root := t.TempDir()
	cli, out := concurrencyTestCLI(t, root)

	existing, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}

	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("name: t\nsteps:\n  - open: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cli.Run(context.Background(), []string{"run", flowPath, "--run", existing.ID}); err != nil {
		t.Fatalf("mav run --run <existing>: %v", err)
	}
	if !strings.Contains(out.String(), "ok cmd=run") {
		t.Fatalf("flow did not succeed: %s", out.String())
	}
	runID := extractField(t, out.String(), "run")
	if runID != existing.ID {
		t.Fatalf("flow reported run=%s, want it to continue the existing run=%s", runID, existing.ID)
	}
}

// TestFlowRunDoesNotStealLiveManualRunPointer covers the write-side
// collision left behind by the read-side M1 fix: runFlow no longer *reads*
// .mav/current-run to decide which run to use, but it still *writes* it on
// entry and on completion, and an unconditional write would silently
// redirect a different agent's live manual `mav open` session (still
// running, its own probe-logs process alive) onto this flow's run.
func TestFlowRunDoesNotStealLiveManualRunPointer(t *testing.T) {
	root := t.TempDir()
	cli, out := concurrencyTestCLI(t, root)

	sleep := exec.Command("sleep", "30")
	sleep.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := sleep.Start(); err != nil {
		t.Fatalf("start manual-session sleep: %v", err)
	}
	manualPID := sleep.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-manualPID, syscall.SIGKILL)
		_ = sleep.Wait()
	})

	manual, err := NewProjectRunState(root)
	if err != nil {
		t.Fatal(err)
	}
	appendProcess(manual, "probe-logs", manualPID, "sleep 30")
	if err := SaveCurrentRun(root, manual); err != nil {
		t.Fatal(err)
	}

	flowPath := filepath.Join(root, "flow.yaml")
	if err := os.WriteFile(flowPath, []byte("name: t\nsteps:\n  - exec: { cmd: 'echo hi' }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AllowShell = true
	if err := SaveConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	if err := cli.Run(context.Background(), []string{"run", flowPath}); err != nil {
		t.Fatalf("mav run: %v", err)
	}
	if !strings.Contains(out.String(), "ok cmd=run") {
		t.Fatalf("flow did not succeed: %s", out.String())
	}
	flowRunID := extractField(t, out.String(), "run")
	if flowRunID == manual.ID {
		t.Fatalf("flow reused the manual session's run id: %s", manual.ID)
	}

	stillCurrent, err := LoadRun(root, "")
	if err != nil {
		t.Fatalf("LoadRun after flow: %v", err)
	}
	if stillCurrent.ID != manual.ID {
		t.Fatalf(".mav/current-run=%s after the flow, want unchanged %s: the flow stole the pointer from a still-live manual session", stillCurrent.ID, manual.ID)
	}
}
