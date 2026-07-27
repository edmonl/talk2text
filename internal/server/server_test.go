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

type decodedResponse struct {
	Ok    bool          `json:"ok"`
	Error string        `json:"error,omitempty"`
	Data  *fakeResponse `json:"data,omitempty"`
}

func newFakeHandler() *fakeHandler {
	h := &fakeHandler{}
	h.logger = log.New(&h.logs, "", 0)
	return h
}

func (h *fakeHandler) Request(request Request) (any, error) {
	h.record(request.Command)
	switch request.Command {
	case "start":
		return nil, h.startErr
	case "stop":
		return nil, h.stopErr
	case "status":
		return &fakeResponse{Command: "status"}, nil
	default:
		return nil, errors.New("unknown command")
	}
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

func TestReadRequestMaxBytesBoundaryUsesFirstJSONValue(t *testing.T) {
	if _, err := readRequest(bytes.NewReader(requestWithSize(maxRequestBytes))); err != nil {
		t.Fatal(err)
	}
	withTrailingValue := append(requestWithSize(maxRequestBytes), []byte(` {"command":"status"}`)...)
	request, err := readRequest(bytes.NewReader(withTrailingValue))
	if err != nil {
		t.Fatal(err)
	}
	if request.Command != "stop" {
		t.Fatalf("command = %q, want first JSON command stop", request.Command)
	}
	if _, err := readRequest(bytes.NewReader(requestWithSize(maxRequestBytes + 1))); err == nil || !strings.Contains(err.Error(), "invalid or too large JSON request") {
		t.Fatalf("over-limit request error = %v, want invalid or too large JSON request", err)
	}
}

func TestDecodeRequestValidatesSchema(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing command", raw: `{}`, want: "request command is required"},
		{name: "unknown field", raw: `{"command":"status","extra":true}`, want: `unknown field "extra"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := readRequest(strings.NewReader(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("readRequest error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestHandleConnAcceptsMultilineRequestWithoutTerminatingNewline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handleConn(ctx, serverConn, newFakeHandler())
		close(done)
	}()
	go func() {
		clientConn.Write([]byte("{\n  \"command\": \"status\"\n}"))
	}()

	var resp decodedResponse
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Ok || resp.Data == nil || resp.Data.Command != "status" {
		t.Fatalf("response = %#v, want successful status", resp)
	}
	<-done
}

func TestDecodeRequestDuplicateFieldsUseLastValue(t *testing.T) {
	request, err := readRequest(strings.NewReader(`{"command":"start","command":"stop","env":{"DISPLAY":"one","DISPLAY":"two"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.Command != "stop" || request.Env["DISPLAY"] != "two" {
		t.Fatalf("request = %#v, want stop with DISPLAY=two", request)
	}
}

func TestDecodeRequestEnvironmentPresence(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		environment bool
	}{
		{name: "missing", raw: `{"command":"stop"}`},
		{name: "null", raw: `{"command":"stop","env":null}`},
		{name: "empty object", raw: `{"command":"stop","env":{}}`, environment: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := readRequest(strings.NewReader(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got := request.Env != nil; got != test.environment {
				t.Fatalf("environment present = %t, want %t", got, test.environment)
			}
		})
	}
}

func TestHandleConnReturnsProtocolError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	done := make(chan struct{})
	go func() {
		handleConn(ctx, serverConn, newFakeHandler())
		close(done)
	}()
	writeDone := make(chan error, 1)
	go func() {
		_, err := clientConn.Write([]byte("{]\n"))
		writeDone <- err
	}()

	var resp decodedResponse
	if err := json.NewDecoder(clientConn).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Ok || !strings.Contains(resp.Error, "invalid or too large JSON request") {
		t.Fatalf("response = %#v, want invalid or too large JSON request error", resp)
	}
	clientConn.Close()
	<-writeDone
	<-done
}

func requestWithSize(size int) []byte {
	const prefix = `{"command":"stop","env":{"DISPLAY":"`
	const suffix = `"}}`
	return []byte(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
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
	request := Request{Command: command}
	if command == "start" || command == "stop" {
		request.Env = map[string]string{}
	}
	if writeErr := json.NewEncoder(conn).Encode(request); writeErr != nil {
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
