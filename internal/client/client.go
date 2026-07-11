// Package client sends requests to the daemon.
package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/edmonl/talk2text/internal/daemon"
	"github.com/edmonl/talk2text/internal/runtimedir"
	"github.com/edmonl/talk2text/internal/server"
)

// ErrDaemonUnavailable indicates that the client could not connect to the daemon.
var ErrDaemonUnavailable = errors.New("daemon unavailable")

// Run sends a client command to the daemon.
func Run(command, runtimeDir string, stdout io.Writer) error {
	resp, err := sendIPCCommand(runtimeDir, command)
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

func sendIPCCommand(runtimeDir, command string) (server.Response[daemon.Status], error) {
	conn, err := net.DialTimeout("unix", runtimedir.SocketPath(runtimeDir), 250*time.Millisecond)
	if err != nil {
		return server.Response[daemon.Status]{}, fmt.Errorf("%w: %s", ErrDaemonUnavailable, err)
	}
	defer conn.Close()
	if _, sendErr := fmt.Fprintf(conn, "%s\n", command); sendErr != nil {
		return server.Response[daemon.Status]{}, fmt.Errorf("failed to send command: %w", sendErr)
	}
	if dlErr := conn.SetReadDeadline(time.Now().Add(time.Second)); dlErr != nil {
		return server.Response[daemon.Status]{}, fmt.Errorf("failed to set read deadline: %w", dlErr)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return server.Response[daemon.Status]{}, fmt.Errorf("failed to read response: %w", err)
	}
	var resp server.Response[daemon.Status]
	if err := json.Unmarshal(line, &resp); err != nil {
		return server.Response[daemon.Status]{}, fmt.Errorf("failed to parse response: %w", err)
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
		fmt.Fprintf(w, "min_duration: %s\n", resp.Config.MinDuration)
		fmt.Fprintf(w, "max_duration: %s\n", resp.Config.MaxDuration)
		fmt.Fprintf(w, "warm_retention: %s\n", resp.Config.WarmRetention)
		fmt.Fprintf(w, "record_input_device: %s\n", resp.Config.RecordInputDevice)
		fmt.Fprintf(w, "whisper_connect_timeout: %s\n", resp.Config.WhisperConnectTimeout)
		fmt.Fprintf(w, "whisper_request_timeout: %s\n", resp.Config.WhisperRequestTimeout)
	}
}
