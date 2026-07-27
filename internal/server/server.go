// Package server implements the IPC server protocol.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"
)

// Request is one IPC request.
type Request struct {
	Command string            `json:"command"`
	Env     map[string]string `json:"env,omitempty"`
}

type response struct {
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

// Handler executes daemon commands received by the IPC server.
type Handler interface {
	Request(request Request) (any, error)
	// Logger must not return nil.
	Logger() *log.Logger
}

const (
	maxRequestBytes = 16 << 10
	requestTimeout  = 250 * time.Millisecond
)

// Serve listens on socketPath and accepts IPC connections until ctx is done.
func Serve(ctx context.Context, socketPath string, handler Handler) error {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer os.Remove(socketPath)
	defer listener.Close()

	if err := os.Chmod(socketPath, 0o600); err != nil {
		return fmt.Errorf("failed to chmod socket: %w", err)
	}
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("failed to accept connection: %w", err)
			}
		}
		handleConn(ctx, conn, handler)
	}
}

func handleConn(ctx context.Context, conn net.Conn, handler Handler) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	defer close(done)
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(requestTimeout)); err != nil {
		handler.Logger().Printf("failed to set request read deadline: %v", err)
		return
	}
	request, err := readRequest(conn)
	if err != nil {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var netErr net.Error
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &netErr) && netErr.Timeout() {
			handler.Logger().Printf("failed to read request: %v", err)
			return
		}
		writeResponse(conn, handler, response{Error: err.Error()})
		return
	}

	data, err := handler.Request(request)
	if err == nil {
		writeResponse(conn, handler, response{Ok: true, Data: data})
	} else {
		writeResponse(conn, handler, response{Error: err.Error()})
	}
}

func writeResponse(conn net.Conn, handler Handler, resp response) {
	if err := conn.SetWriteDeadline(time.Now().Add(requestTimeout)); err != nil {
		handler.Logger().Printf("failed to set response write deadline: %v", err)
		return
	}
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		handler.Logger().Printf("failed to write response: %v", err)
	}
}

func readRequest(r io.Reader) (Request, error) {
	decoder := json.NewDecoder(io.LimitReader(r, maxRequestBytes))
	decoder.DisallowUnknownFields()

	var request Request
	if err := decoder.Decode(&request); err != nil {
		return Request{}, fmt.Errorf("invalid or too large JSON request: %w", err)
	}
	if request.Command == "" {
		return Request{}, errors.New("request command is required")
	}
	return request, nil
}
