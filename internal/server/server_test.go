package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeHandler struct {
	logger *log.Logger
	logs   bytes.Buffer

	mu    sync.Mutex
	calls []string

	startErr error
	stopErr  error
}

type fakeResponse struct {
	Command string `json:"command,omitempty"`
}

func newFakeHandler() *fakeHandler {
	h := &fakeHandler{}
	h.logger = log.New(&h.logs, "", 0)
	return h
}

func (h *fakeHandler) Request(command string) error {
	h.record(command)
	switch command {
	case "start":
		return h.startErr
	case "stop":
		return h.stopErr
	default:
		return errors.New("unknown command")
	}
}

func (h *fakeHandler) Status() *fakeResponse {
	h.record("status")
	return &fakeResponse{Command: "status"}
}

func (h *fakeHandler) Logger() *log.Logger {
	return h.logger
}

func (h *fakeHandler) record(command string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, command)
}

func TestServeDispatchesCommands(t *testing.T) {
	socketPath, cancel, done := startTestServer(t)
	defer stopTestServer(t, cancel, done)

	for _, command := range []string{"start", "stop", "status"} {
		resp := sendCommand(t, socketPath, command)
		if resp["ok"] != true {
			t.Fatalf("%s response ok = %v, want true", command, resp["ok"])
		}
		if command == "status" {
			data, ok := resp["data"].(map[string]any)
			if !ok {
				t.Fatalf("status response data = %T, want object", resp["data"])
			}
			if data["command"] != command {
				t.Fatalf("status response data.command = %v, want %q", data["command"], command)
			}
		}
	}
}

func TestServeReturnsErrorForUnknownCommand(t *testing.T) {
	socketPath, cancel, done := startTestServer(t)
	defer stopTestServer(t, cancel, done)

	resp := sendCommand(t, socketPath, "bogus")
	if resp["ok"] != false {
		t.Fatalf("unknown response ok = %v, want false", resp["ok"])
	}
	if resp["error"] != "unknown command" {
		t.Fatalf("unknown response error = %v, want unknown command", resp["error"])
	}
}

func TestServeReturnsHandlerError(t *testing.T) {
	socketPath, cancel, done := startTestServer(t, &fakeHandler{
		startErr: errors.New("start failed"),
	})
	defer stopTestServer(t, cancel, done)

	resp := sendCommand(t, socketPath, "start")
	if resp["ok"] != false {
		t.Fatalf("start response ok = %v, want false", resp["ok"])
	}
	if resp["error"] != "start failed" {
		t.Fatalf("start response error = %v, want start failed", resp["error"])
	}
}

func TestServeHandlesNextConnectionWhileRequestIsIncomplete(t *testing.T) {
	socketPath, cancel, done := startTestServer(t)
	defer stopTestServer(t, cancel, done)

	slowConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer slowConn.Close()

	type commandResult struct {
		resp map[string]any
		err  error
	}
	quickDone := make(chan commandResult, 1)
	go func() {
		resp, err := sendCommandResult(socketPath, "status")
		quickDone <- commandResult{resp: resp, err: err}
	}()

	select {
	case result := <-quickDone:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.resp["ok"] != true {
			t.Fatalf("status response ok = %v, want true", result.resp["ok"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("complete request was blocked behind incomplete request")
	}
}

func TestServeClosesIncompleteRequestAfterTimeout(t *testing.T) {
	socketPath, cancel, done := startTestServer(t)
	defer stopTestServer(t, cancel, done)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	start := time.Now()
	raw, err := io.ReadAll(conn)
	if err != nil && !errors.Is(err, syscall.ECONNRESET) {
		t.Fatal(err)
	}
	if len(raw) != 0 {
		t.Fatalf("incomplete request response = %q, want closed connection without response", string(raw))
	}
	if elapsed := time.Since(start); elapsed > 2*requestTimeout {
		t.Fatalf("incomplete request closed after %s, want within %s", elapsed, 2*requestTimeout)
	}
}

func TestServeDropsOversizedRequest(t *testing.T) {
	socketPath, cancel, done := startTestServer(t)
	defer stopTestServer(t, cancel, done)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, writeErr := conn.Write([]byte(strings.Repeat("x", maxRequestBytes+1))); writeErr != nil {
		t.Fatal(writeErr)
	}
	raw, err := io.ReadAll(conn)
	if err != nil && !errors.Is(err, syscall.ECONNRESET) {
		t.Fatal(err)
	}
	if len(raw) != 0 {
		t.Fatalf("oversized response = %q, want closed connection without response", string(raw))
	}
}

func TestReadCommandMaxRequestBytesBoundary(t *testing.T) {
	command, err := readCommand(strings.NewReader(strings.Repeat("x", maxRequestBytes-1) + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if command != strings.Repeat("x", maxRequestBytes-1) {
		t.Fatalf("command = %q, want exact boundary command", command)
	}

	if _, err := readCommand(strings.NewReader(strings.Repeat("x", maxRequestBytes) + "\n")); err == nil {
		t.Fatal("over-limit command succeeded, want error")
	}
}

func TestHandleConnReturnsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handleConn(ctx, serverConn, newFakeHandler())
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleConn did not return after context cancellation")
	}
}

func TestServeRemovesSocketOnCancel(t *testing.T) {
	socketPath, cancel, done := startTestServer(t)
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatalf("socket does not exist before cancel: %v", err)
	}

	stopTestServer(t, cancel, done)
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatalf("socket still exists or stat failed differently after cancel: %v", err)
	}
}

func startTestServer(t *testing.T, handlers ...*fakeHandler) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "server.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	handler := newFakeHandler()
	if len(handlers) > 0 {
		handler = handlers[0]
		if handler.logger == nil {
			handler.logger = log.New(&handler.logs, "", 0)
		}
	}
	go func() {
		done <- Serve(ctx, socketPath, handler)
	}()
	waitForSocket(t, socketPath, done)
	return socketPath, cancel, done
}

func stopTestServer(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after cancellation")
	}
}

func waitForSocket(t *testing.T, socketPath string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(socketPath); err == nil {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("Serve returned before socket was ready: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created", socketPath)
}

func sendCommand(t *testing.T, socketPath, command string) map[string]any {
	t.Helper()
	resp, err := sendCommandResult(socketPath, command)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func sendCommandResult(socketPath, command string) (map[string]any, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, writeErr := conn.Write([]byte(command + "\n")); writeErr != nil {
		return nil, writeErr
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}
