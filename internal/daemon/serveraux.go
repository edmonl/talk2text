package daemon

import (
	"errors"
	"fmt"
	"log"

	"github.com/edmonl/talk2text/internal/daemon/config"
	"github.com/edmonl/talk2text/internal/server"
)

// Status is the daemon status data sent over IPC.
type Status struct {
	State      string `json:"state,omitempty"`
	NextClipID int    `json:"next_clip_id,omitempty"`
	ActiveClip int    `json:"active_clip_id,omitempty"`
	// todo: need a better name for this
	Pending int32          `json:"pending_transcriptions,omitempty"`
	Config  *config.Config `json:"config,omitempty"`
}

// Status reports the daemon state.
func (d *daemon) Status() *Status {
	status := &Status{
		State:   "off",
		Pending: d.ongoingTranscriptions.Load(),
		Config:  d.cfg,
	}

	d.muCapture.Lock()
	defer d.muCapture.Unlock()

	status.NextClipID = d.nextClip
	if d.active != nil {
		status.State = "active"
		status.ActiveClip = d.active.ID()
	} else if d.desiredStreamState == streamWarm {
		status.State = "warm"
	}

	return status
}

// Logger returns the daemon logger.
func (d *daemon) Logger() *log.Logger {
	return d.log
}

func (d *daemon) Request(cmd string) error {
	if d.ctx.Err() != nil {
		return errors.New("daemon shutting down")
	}

	errChan := make(chan error, 1)
	switch cmd {
	case "start":
		go d.start(errChan)
	case "stop":
		go d.stop(errChan)
	default:
		return errors.New("unknown command")
	}

	return <-errChan
}

func (d *daemon) serveSocket(socketPath string) error {
	d.log.Printf("daemon starting to listen on %s", socketPath)
	if err := server.Serve(d.ctx, socketPath, d); err != nil {
		return fmt.Errorf("failed to start daemon on socket %s: %w", socketPath, err)
	}

	return nil
}

func (d *daemon) serve(socketPath string) error {
	serverErrors := make(chan error)

	serverCount := 1
	go func() {
		serverErrors <- d.serveSocket(socketPath)
	}()

	if d.cfg.HTTPListen != "" {
		serverCount++
		go func() {
			serverErrors <- d.serveHTTP()
		}()
	}

	var serveErr error
	for completed := 0; completed < serverCount; completed++ {
		err := <-serverErrors
		if err != nil {
			serveErr = err
			d.cancel()
		}
	}

	return serveErr
}
