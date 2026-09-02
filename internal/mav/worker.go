package mav

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type workerRequest struct {
	Command string               `json:"command"`
	UDID    string               `json:"udid,omitempty"`
	Events  []workerGestureEvent `json:"events,omitempty"`
	Args    map[string]string    `json:"args,omitempty"`
}

type workerGestureEvent struct {
	JSON    string `json:"json"`
	DelayMs int    `json:"delay_ms,omitempty"`
}

type workerResponse struct {
	OK      bool     `json:"ok"`
	Error   string   `json:"error,omitempty"`
	Results []string `json:"results,omitempty"`
}

type baguetteInputSession struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	mu      sync.Mutex
}

type runWorker struct {
	socket   string
	listener net.Listener
	sessions map[string]*baguetteInputSession
	debug    *dapClient
	lease    time.Duration
	lastSeen time.Time
	mu       sync.Mutex
	inode    uint64
	hasInode bool
}

const (
	defaultWorkerLease      = 15 * time.Minute
	workerHeartbeatInterval = time.Minute
)

// maxUnixSocketPath is the longest path a Unix domain socket may be bound
// to. sockaddr_un.sun_path is 104 bytes on Darwin and 108 on Linux, and the
// path is NUL-terminated inside it, so the usable length is one less than
// the smaller of the two — kept uniform across platforms so the fallback
// below is exercised identically everywhere rather than only on the machine
// that happens to have the tighter limit.
//
// Nothing about this is theoretical. A run directory inside a git worktree
// (.../<repo>/.claude/worktrees/<branch>/.mav/runs/<id>/worker.sock)
// measured 106 bytes in the wild, two over the limit, and every worker
// startup in that tree failed with "bind: invalid argument" — silently,
// because startRunWorker only degrades to "direct" mode and logs one line.
// The worker is what watches a run's lease and reaps it when nobody renews,
// so losing it means an interrupted run leaves its `log stream` and its
// simulator behind with nothing left to collect them.
const maxUnixSocketPath = 103

// workerSocket is where a run's worker listens. Normally that is inside the
// run directory, which keeps it beside the run's other state and disposed of
// with it. When that path would not fit in sun_path, it falls back to a
// short path under the system temp directory, derived from the run directory
// so every mav process working on the same run independently computes the
// same socket, and two runs never collapse onto one.
func workerSocket(run RunState) string {
	natural := filepath.Join(run.Dir, "worker.sock")
	if len(natural) <= maxUnixSocketPath {
		return natural
	}
	// run.Dir and the system temp dir are both process-local (os.Getwd
	// applies the $PWD kludge, and $TMPDIR varies by caller), so two
	// processes working on the same physical run directory could otherwise
	// compute two different fallback sockets. Canonicalize the directory and
	// pin a fixed, uid-scoped base so every process working on the same run
	// converges on the same path regardless of symlink spelling or TMPDIR.
	dir := run.Dir
	if resolved, err := filepath.EvalSymlinks(run.Dir); err == nil {
		dir = resolved
	}
	sum := sha256.Sum256([]byte(dir))
	name := fmt.Sprintf("mav-worker-%d-%s.sock", os.Getuid(), hex.EncodeToString(sum[:])[:16])
	return filepath.Join("/tmp", name)
}

func workerStartLock(run RunState) string { return filepath.Join(run.Dir, "worker.starting") }

// workerLockStaleAge bounds how long a worker.starting lock file is honored
// before it is treated as abandoned and stolen. A legitimate holder only
// needs it for the ~2s of cmd.Start()+ping polling below, so this is set well
// above that: if the holder was killed (SIGKILL/OOM/crash) before its defer
// ran, the lock would otherwise sit on disk forever and permanently force
// every future startRunWorker call for that run into "direct" mode.
const workerLockStaleAge = 10 * time.Second

func acquireWorkerLock(lockPath string) (*os.File, error) {
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		return lock, nil
	}
	if !os.IsExist(err) {
		return nil, err
	}
	info, statErr := os.Stat(lockPath)
	if statErr != nil || time.Since(info.ModTime()) < workerLockStaleAge {
		return nil, err
	}
	// The lock is older than any legitimate holder should need; assume its
	// owner died mid-start and steal it.
	_ = os.Remove(lockPath)
	return os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

func startRunWorker(root string, run RunState) (string, error) {
	if workerPing(run) {
		return "worker", nil
	}
	// Serialize concurrent spawns (e.g. two mav commands racing after both
	// see a dead socket) with an exclusive lock file, so only one process
	// removes the stale socket and starts a replacement worker.
	lockPath := workerStartLock(run)
	lock, err := acquireWorkerLock(lockPath)
	if err != nil {
		// Another mav process is already starting a worker; give it time to
		// come up rather than racing to spawn a second one.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if workerPing(run) {
				return "worker", nil
			}
			time.Sleep(25 * time.Millisecond)
		}
		return "direct", fmt.Errorf("worker_start_timeout")
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lockPath)
	}()
	if workerPing(run) {
		return "worker", nil
	}
	_ = os.Remove(workerSocket(run))
	executable, err := os.Executable()
	if err != nil {
		return "direct", err
	}
	logPath := filepath.Join(run.Dir, "worker.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "direct", err
	}
	cmd := exec.Command(
		executable,
		"__worker",
		"--socket", workerSocket(run),
		"--root", root,
		"--run", run.ID,
		"--lease", defaultWorkerLease.String(),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return "direct", err
	}
	go func() {
		_ = cmd.Wait()
		_ = log.Close()
	}()
	if cmd.Process != nil {
		// Host: the worker is started here even when the app under test is
		// in a VM, because what it watches -- the run's lease -- is here.
		appendHostProcess(run, "worker", cmd.Process.Pid, executable+" __worker")
		_ = os.WriteFile(filepath.Join(run.Dir, "worker.pid"), []byte(fmt.Sprint(cmd.Process.Pid)), 0o644)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if workerPing(run) {
			return "worker", nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return "direct", fmt.Errorf("worker_start_timeout")
}

func workerPing(run RunState) bool {
	response, err := sendWorkerRequest(run, workerRequest{Command: "ping"})
	return err == nil && response.OK
}

func sendWorkerRequest(run RunState, request workerRequest) (workerResponse, error) {
	conn, err := net.DialTimeout("unix", workerSocket(run), 300*time.Millisecond)
	if err != nil {
		return workerResponse{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return workerResponse{}, err
	}
	var response workerResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return workerResponse{}, err
	}
	if !response.OK {
		return response, fmt.Errorf("%s", response.Error)
	}
	return response, nil
}

func sendWorkerGesture(run RunState, udid string, events []workerGestureEvent) error {
	_, err := sendWorkerRequest(run, workerRequest{Command: "baguette", UDID: udid, Events: events})
	return err
}

func (c CLI) runInternalWorker(ctx context.Context, args []string) error {
	socket := flagValue(args, "--socket")
	if socket == "" {
		return fmt.Errorf("worker_socket_missing")
	}
	lease := defaultWorkerLease
	if value := flagValue(args, "--lease"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return fmt.Errorf("worker_lease_invalid")
		}
		lease = parsed
	}
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		_ = listener.Close()
		return err
	}
	worker := &runWorker{
		socket: socket, listener: listener, sessions: map[string]*baguetteInputSession{},
		lease: lease, lastSeen: time.Now(),
	}
	if ino, ok := socketInode(socket); ok {
		worker.inode, worker.hasInode = ino, true
	}
	expired := false
	for {
		if unixListener, ok := listener.(*net.UnixListener); ok {
			_ = unixListener.SetDeadline(worker.leaseDeadline())
		}
		conn, err := listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if worker.leaseExpired() {
					expired = true
					break
				}
				continue
			}
			select {
			case <-ctx.Done():
				worker.close()
				return nil
			default:
				worker.close()
				return err
			}
		}
		worker.renewLease()
		stop := worker.handle(conn)
		worker.renewLease()
		_ = conn.Close()
		if stop {
			break
		}
	}
	// Check ownership before close() removes the socket, so a worker that
	// has already been superseded by a replacement (its socket path now
	// resolves to someone else's listener) never tears down a run that is
	// actively being served by that replacement.
	owned := worker.ownsSocket()
	worker.close()
	if expired && owned {
		root := flagValue(args, "--root")
		runID := flagValue(args, "--run")
		if root != "" && runID != "" {
			run, loadErr := LoadRun(root, runID)
			if loadErr == nil {
				appendFile(run.LogsPath, "mav worker lease expired after "+lease.String()+"; cleaning run\n")
				_ = os.WriteFile(filepath.Join(run.Dir, "lease.expired"), []byte(time.Now().Format(time.RFC3339)+"\n"), 0o600)
				// reapAbandonedRun, not stop: this is the one place a run's
				// owner is confirmed gone (nobody renewed the lease), so an
				// in-flight video recording -- which stop() alone leaves
				// running -- must be killed here too, or it never dies.
				c.withStdout(io.Discard).reapAbandonedRun(ctx, run)
			}
		}
	}
	return nil
}

func (w *runWorker) handle(conn net.Conn) bool {
	var request workerRequest
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(workerResponse{Error: err.Error()})
		return false
	}
	response := workerResponse{OK: true}
	stop := false
	switch request.Command {
	case "ping", "renew":
	case "stop":
		stop = true
	case "baguette":
		results, err := w.sendBaguette(request.UDID, request.Events)
		if err != nil {
			response.OK = false
			response.Error = err.Error()
		} else {
			response.Results = results
		}
	case "debug":
		results, err := w.handleDebug(request.Args)
		if err != nil {
			response.OK = false
			response.Error = err.Error()
		} else {
			response.Results = results
		}
	default:
		response.OK = false
		response.Error = "worker_command_unknown"
	}
	_ = json.NewEncoder(conn).Encode(response)
	return stop
}

func (w *runWorker) renewLease() {
	w.mu.Lock()
	w.lastSeen = time.Now()
	w.mu.Unlock()
}

func (w *runWorker) leaseDeadline() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastSeen.Add(w.lease)
}

func (w *runWorker) leaseExpired() bool {
	return !time.Now().Before(w.leaseDeadline())
}

// baguetteAckTimeout bounds each wait for a baguette input ack line, so a
// wedged simulator or stuck FB framework call cannot hang the worker's
// single-threaded accept loop (and defeat lease-expiry cleanup) forever.
const baguetteAckTimeout = 30 * time.Second

// scanBaguetteAck reads one ack line from scanner behind a goroutine so the
// caller never blocks past timeout. The abandoned goroutine's result is
// simply dropped if it arrives late; the caller is expected to kill the
// underlying process on timeout, which unblocks it.
func scanBaguetteAck(scanner *bufio.Scanner, timeout time.Duration) (string, bool) {
	type result struct {
		ok   bool
		line string
	}
	ch := make(chan result, 1)
	go func() {
		ok := scanner.Scan()
		ch <- result{ok: ok, line: scanner.Text()}
	}()
	select {
	case r := <-ch:
		return r.line, r.ok
	case <-time.After(timeout):
		return "", false
	}
}

func (w *runWorker) sendBaguette(udid string, events []workerGestureEvent) ([]string, error) {
	if udid == "" || len(events) == 0 {
		return nil, fmt.Errorf("worker_gesture_invalid")
	}
	session, err := w.baguetteSession(udid)
	if err != nil {
		return nil, err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	results := make([]string, 0, len(events))
	for _, event := range events {
		if event.DelayMs > 0 {
			time.Sleep(time.Duration(event.DelayMs) * time.Millisecond)
		}
		if _, err := io.WriteString(session.stdin, event.JSON+"\n"); err != nil {
			w.dropSession(udid)
			return nil, err
		}
		line, ok := scanBaguetteAck(session.scanner, baguetteAckTimeout)
		if !ok {
			w.dropSession(udid)
			return nil, fmt.Errorf("baguette_input_closed")
		}
		results = append(results, line)
		var ack struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &ack); err != nil || !ack.OK {
			return results, fmt.Errorf("baguette_input_rejected: %s", line)
		}
	}
	return results, nil
}

func (w *runWorker) baguetteSession(udid string) (*baguetteInputSession, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if session := w.sessions[udid]; session != nil {
		return session, nil
	}
	cmd := exec.Command("baguette", "input", "--udid", udid)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	session := &baguetteInputSession{cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout)}
	w.sessions[udid] = session
	return session, nil
}

func (w *runWorker) dropSession(udid string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if session := w.sessions[udid]; session != nil {
		_ = session.stdin.Close()
		if session.cmd.Process != nil {
			_ = session.cmd.Process.Kill()
		}
		go func() { _ = session.cmd.Wait() }()
		delete(w.sessions, udid)
	}
}

// socketInode stats path and returns the inode backing it, so callers can
// tell whether the path still refers to the same socket file they bound
// (as opposed to one a replacement worker has since created in its place).
func socketInode(path string) (uint64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Ino, true
}

// ownsSocket reports whether w.socket still resolves to the file this
// worker originally bound. It returns true when no baseline inode was
// captured (best effort, e.g. on platforms without syscall.Stat_t).
func (w *runWorker) ownsSocket() bool {
	if !w.hasInode {
		return true
	}
	ino, ok := socketInode(w.socket)
	return ok && ino == w.inode
}

func (w *runWorker) close() {
	w.mu.Lock()
	for _, session := range w.sessions {
		_ = session.stdin.Close()
		if session.cmd.Process != nil {
			_ = session.cmd.Process.Kill()
		}
		go func(s *baguetteInputSession) { _ = s.cmd.Wait() }(session)
	}
	w.sessions = map[string]*baguetteInputSession{}
	w.mu.Unlock()
	if w.debug != nil {
		_ = w.debug.close()
	}
	_ = w.listener.Close()
	// Only unlink the socket path if it still points at the file we bound;
	// otherwise a replacement worker has already taken over this path and
	// removing it would orphan that worker instead.
	if w.ownsSocket() {
		_ = os.Remove(w.socket)
	}
}
