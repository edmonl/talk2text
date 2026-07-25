// Package server implements the IPC server protocol.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

type Response[DataType any] struct {
	Ok    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
	Data  *DataType `json:"data,omitempty"`
}

// Handler executes daemon commands received by the IPC server.
type Handler[StatusType any] interface {
	Request(cmd string) error
	Status() *StatusType
	// Logger must not return nil.
	Logger() *log.Logger
}

const (
	maxRequestBytes = 10
	requestTimeout  = 250 * time.Millisecond
)

// Serve listens on socketPath and accepts IPC connections until ctx is done.
func Serve[StatusType any](ctx context.Context, socketPath string, handler Handler[StatusType]) error {
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

func handleConn[StatusType any](ctx context.Context, conn net.Conn, handler Handler[StatusType]) {
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
	command, err := readCommand(conn)
	if err != nil {
		select {
		case <-ctx.Done():
			return
		default:
		}
		handler.Logger().Printf("failed to read request: %v", err)
		return
	}

	var resp Response[StatusType]
	switch command {
	case "status":
		status := handler.Status()
		if status == nil {
			resp = Response[StatusType]{Error: "failed to get status"}
		} else {
			resp = Response[StatusType]{
				Ok:   true,
				Data: status,
			}
		}
	default:
		resp = getRespByError[StatusType](handler.Request(command))
	}

	if err := conn.SetWriteDeadline(time.Now().Add(requestTimeout)); err != nil {
		handler.Logger().Printf("failed to set response write deadline: %v", err)
		return
	}
	err = json.NewEncoder(conn).Encode(resp)
	if err != nil {
		handler.Logger().Printf("failed to write response: %v", err)
	}
}

func getRespByError[DataType any](err error) Response[DataType] {
	if err == nil {
		return Response[DataType]{Ok: true}
	}

	return Response[DataType]{Error: err.Error()}
}

func readCommand(r io.Reader) (string, error) {
	limited := &io.LimitedReader{R: r, N: maxRequestBytes}
	line, err := bufio.NewReader(limited).ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && limited.N == 0 {
			return "", errors.New("request too large")
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}
