// Package client sends requests to the daemon.
package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/edmonl/talk2text/internal/daemon"
	"github.com/edmonl/talk2text/internal/requestenv"
	"github.com/edmonl/talk2text/internal/runtimedir"
	"github.com/edmonl/talk2text/internal/server"
)

// ErrDaemonUnavailable indicates that the client could not connect to the daemon.
var ErrDaemonUnavailable = errors.New("daemon unavailable")

var startAutomaticNames = []string{
	requestenv.SessionIDName,
	requestenv.XDGSessionIDName,
	requestenv.DisplayName,
	requestenv.WaylandDisplayName,
	requestenv.XAuthorityName,
	requestenv.DBusSessionBusAddressName,
	requestenv.XDGRuntimeDirName,
	requestenv.XDGSessionTypeName,
	requestenv.XDGSessionDesktopName,
	requestenv.XDGCurrentDesktopName,
}

var stopAutomaticNames = []string{
	requestenv.SessionIDName,
	requestenv.XDGSessionIDName,
}

// Config contains client command options.
type Config struct {
	// RuntimeDir contains the daemon socket.
	RuntimeDir string
	// SendEnv contains additional environment variable names sent by start.
	SendEnv []string
}

type response struct {
	Ok    bool           `json:"ok"`
	Error string         `json:"error,omitempty"`
	Data  *daemon.Status `json:"data,omitempty"`
}

// Run sends a client command to the daemon.
func Run(command string, cfg Config, stdout io.Writer) error {
	request, err := buildRequest(command, cfg.SendEnv)
	if err != nil {
		return err
	}
	resp, err := sendIPCRequest(cfg.RuntimeDir, request)
	if err != nil {
		return err
	}
	if !resp.Ok {
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		return errors.New("command failed")
	}
	if command == "status" {
		if resp.Data == nil {
			return errors.New("status response missing data")
		}
		printStatus(stdout, *resp.Data)
	}
	return nil
}

func buildRequest(command string, sendEnv []string) (server.Request, error) {
	request := server.Request{Command: command}
	var names []string
	switch command {
	case "start":
		names = startAutomaticNames[:]
	case "stop":
		names = stopAutomaticNames[:]
	default:
		return request, nil
	}
	names = append(names, sendEnv...)

	environment := make(map[string]string)
	for _, name := range names {
		value, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		if err := requestenv.ValidateValue(value); err != nil {
			return server.Request{}, fmt.Errorf("invalid environment variable %s: %w", name, err)
		}
		environment[name] = value
	}
	request.Env = environment
	return request, nil
}

func sendIPCRequest(runtimeDir string, request server.Request) (response, error) {
	conn, err := net.DialTimeout("unix", runtimedir.SocketPath(runtimeDir), 250*time.Millisecond)
	if err != nil {
		return response{}, fmt.Errorf("%w: %v", ErrDaemonUnavailable, err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return response{}, fmt.Errorf("failed to send command: %w", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return response{}, fmt.Errorf("failed to set read deadline: %w", err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return response{}, fmt.Errorf("failed to read response: %w", err)
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return response{}, fmt.Errorf("failed to parse response: %w", err)
	}
	return resp, nil
}

func printStatus(w io.Writer, resp daemon.Status) {
	fmt.Fprintf(w, "state: %s\n", resp.State)
	fmt.Fprintf(w, "next_clip_id: %d\n", resp.NextClipID)
	if resp.ActiveClip != 0 {
		fmt.Fprintf(w, "active_clip_id: %d\n", resp.ActiveClip)
	}
	fmt.Fprintf(w, "pending_transcriptions: %d\n", resp.Pending)
	if resp.Config != nil {
		fmt.Fprintf(w, "runtime_dir: %s\n", resp.Config.RuntimeDir)
		fmt.Fprintf(w, "whisper_endpoint: %s\n", resp.Config.WhisperEndpoint)
		fmt.Fprintf(w, "out_cmd: %s\n", resp.Config.OutCmd)
		fmt.Fprintf(w, "notify_cmd: %s\n", resp.Config.NotifyCmd)
		fmt.Fprintf(w, "http_listen: %s\n", resp.Config.HTTPListen)
		fmt.Fprintf(w, "allow_client_env: %s\n", strings.Join(resp.Config.AllowClientEnv, ","))
		fmt.Fprintf(w, "min_duration: %s\n", resp.Config.MinDuration)
		fmt.Fprintf(w, "max_duration: %s\n", resp.Config.MaxDuration)
		fmt.Fprintf(w, "stop_delay: %s\n", resp.Config.StopDelay)
		fmt.Fprintf(w, "warm_retention: %s\n", resp.Config.WarmRetention)
		fmt.Fprintf(w, "transcript_retention_window: %d\n", resp.Config.TranscriptRetentionWindow)
		fmt.Fprintf(w, "record_input_device: %s\n", resp.Config.RecordInputDevice)
		fmt.Fprintf(w, "whisper_connect_timeout: %s\n", resp.Config.WhisperConnectTimeout)
		fmt.Fprintf(w, "whisper_request_timeout: %s\n", resp.Config.WhisperRequestTimeout)
	}
}
