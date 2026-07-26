package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"time"

	"github.com/edmonl/talk2text/internal/amrwb"
	"github.com/edmonl/talk2text/internal/daemon/session"
)

const (
	amrwbFrameDuration       = 20 * time.Millisecond
	amrwbMaxEncodedFrameSize = 61
	maxHTTPAdmitted          = 2
)

type httpResponse struct {
	Ok     bool   `json:"ok"`
	ClipID int    `json:"clip_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (d *daemon) serveHTTP() error {
	listener, err := net.Listen("tcp", d.cfg.HTTPListen)
	if err != nil {
		return fmt.Errorf("failed to serve HTTP on %s: %w", d.cfg.HTTPListen, err)
	}

	httpServer := &http.Server{
		Handler: http.HandlerFunc(d.handleHTTP),
		BaseContext: func(net.Listener) context.Context {
			return d.ctx
		},

		ReadHeaderTimeout: time.Second,
		MaxHeaderBytes:    8 << 10, // 8 KiB
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
		ErrorLog:          d.log,
	}
	go func() {
		<-d.ctx.Done()
		httpServer.Close()
	}()

	d.log.Printf("daemon starting to listen on %s", listener.Addr())
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (d *daemon) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/transcribe" {
		writeHTTPJSON(w, http.StatusNotFound, httpResponse{Error: "not found"})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeHTTPJSON(w, http.StatusMethodNotAllowed, httpResponse{Error: "method not allowed"})
		return
	}
	if d.ctx.Err() != nil {
		writeHTTPJSON(w, http.StatusServiceUnavailable, httpResponse{Error: "shutting down"})
		return
	}
	if d.httpAdmitted.Add(1) > maxHTTPAdmitted {
		d.httpAdmitted.Add(-1)
		writeHTTPJSON(w, http.StatusServiceUnavailable, httpResponse{Error: "busy"})
		return
	}

	unfinished := true
	defer func() {
		if unfinished {
			d.httpAdmitted.Add(-1)
		}
	}()

	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "audio/amr-wb" {
		writeHTTPJSON(w, http.StatusUnsupportedMediaType, httpResponse{Error: "content type must be audio/amr-wb"})
		return
	}

	maxFrames, maxBodyBytes := httpAudioLimits(d.cfg.MaxDuration)
	if r.ContentLength > maxBodyBytes {
		writeHTTPJSON(w, http.StatusRequestEntityTooLarge, httpResponse{Error: "body too large"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeHTTPJSON(w, http.StatusRequestEntityTooLarge, httpResponse{Error: "body too large"})
			return
		}
		writeHTTPJSON(w, http.StatusBadRequest, httpResponse{Error: "failed to read request body"})
		return
	}

	pcm, err := amrwb.Decode(body, maxFrames)
	if err != nil {
		switch {
		case errors.Is(err, amrwb.ErrTooLong):
			writeHTTPJSON(w, http.StatusRequestEntityTooLarge, httpResponse{Error: err.Error()})
		case errors.Is(err, amrwb.ErrInvalidHeader), errors.Is(err, amrwb.ErrInvalidFrame):
			writeHTTPJSON(w, http.StatusBadRequest, httpResponse{Error: err.Error()})
		default:
			d.log.Printf("failed to decode HTTP audio: %v", err)
			writeHTTPJSON(w, http.StatusInternalServerError, httpResponse{Error: "failed to decode audio"})
		}
		return
	}
	if d.ctx.Err() != nil {
		writeHTTPJSON(w, http.StatusServiceUnavailable, httpResponse{Error: "daemon shutting down"})
		return
	}

	d.muCapture.Lock()
	clipID := d.nextClip
	d.nextClip++
	d.muCapture.Unlock()

	s, err := session.NewSessionWithPCM(clipID, pcm)
	if err != nil {
		d.log.Printf("failed to accept HTTP audio for clip %d: %v", clipID, err)
		writeHTTPJSON(w, http.StatusInternalServerError, httpResponse{Error: "failed to accept audio"})
		return
	}

	unfinished = false
	go d.transcribeHTTP(s)
	writeHTTPJSON(w, http.StatusAccepted, httpResponse{Ok: true, ClipID: clipID})
}

func httpAudioLimits(maxDuration time.Duration) (int, int64) {
	frameCount := min(int64(maxDuration/amrwbFrameDuration), int64(math.MaxInt))
	maxBodyBytes := int64(len(amrwb.Magic)) + frameCount*amrwbMaxEncodedFrameSize
	return int(frameCount), maxBodyBytes
}

func writeHTTPJSON(w http.ResponseWriter, status int, response httpResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func (d *daemon) transcribeHTTP(s *session.Session) {
	if d.isShortSession(s) {
		d.httpAdmitted.Add(-1)
		d.processTranscript(s.ID(), "", false)
		return
	}

	success := d.waitForLocalTranscriptions()
	d.httpAdmitted.Add(-1)
	if success {
		d.processLongSession(s)
	}
}

func (d *daemon) waitForLocalTranscriptions() bool {
	for !d.ongoingTranscriptions.CompareAndSwap(0, 1) {
		select {
		case <-d.transcriptionIdle:
		case <-d.ctx.Done():
			return false
		}
	}

	return true
}
