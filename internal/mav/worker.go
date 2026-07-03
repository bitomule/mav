package mav

import (
	"bufio"
	"context"
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
}

const (
	defaultWorkerLease      = 15 * time.Minute
	workerHeartbeatInterval = time.Minute
)

func workerSocket(run RunState) string { return filepath.Join(run.Dir, "worker.sock") }

func startRunWorker(root string, run RunState) (string, error) {
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
		appendProcess(run, "worker", cmd.Process.Pid, executable+" __worker")
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
	worker.close()
	if expired {
		root := flagValue(args, "--root")
		runID := flagValue(args, "--run")
		if root != "" && runID != "" {
			run, loadErr := LoadRun(root, runID)
			if loadErr == nil {
				appendFile(run.LogsPath, "mav worker lease expired after "+lease.String()+"; cleaning run\n")
				_ = os.WriteFile(filepath.Join(run.Dir, "lease.expired"), []byte(time.Now().Format(time.RFC3339)+"\n"), 0o600)
				_ = c.withStdout(io.Discard).stop(ctx, GlobalOptions{}, []string{"--run", run.ID})
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
		if !session.scanner.Scan() {
			w.dropSession(udid)
			return nil, fmt.Errorf("baguette_input_closed")
		}
		line := session.scanner.Text()
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
		delete(w.sessions, udid)
	}
}

func (w *runWorker) close() {
	w.mu.Lock()
	for _, session := range w.sessions {
		_ = session.stdin.Close()
		if session.cmd.Process != nil {
			_ = session.cmd.Process.Kill()
		}
	}
	w.sessions = map[string]*baguetteInputSession{}
	w.mu.Unlock()
	if w.debug != nil {
		_ = w.debug.close()
	}
	_ = w.listener.Close()
	_ = os.Remove(w.socket)
}
