package daemon

import (
	"github.com/edmonl/talk2text/internal/audiocapture"
)

func (d *daemon) openStream() error {
	if d.stream != nil {
		return nil
	}

	stream, err := d.newAudio(audiocapture.Config{
		InputDevice: d.cfg.RecordInputDevice,
		OnPCM:       d.onAudio,
	})
	if err != nil {
		return err
	}

	d.stream = stream
	return nil
}

func (d *daemon) closeStream() {
	stream := d.stream
	d.stream = nil
	if err := stream.Close(); err != nil {
		d.log.Printf("failed to close audio capture: %v", err)
	}
}

type streamState int

const (
	streamOff streamState = iota
	streamWarm
	streamRecording
)

func (d *daemon) desireStream(state streamState) {
	d.desiredStreamState = state
	select {
	case d.desiredStreamStateSignal <- struct{}{}:
	default:
	}
}

func (d *daemon) streamManager() {
	defer close(d.streamManagerDone)
	defer func() {
		if d.stream != nil {
			d.closeStream()
		}
	}()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.desiredStreamStateSignal:
		}

		d.muCapture.Lock()
		desired := d.desiredStreamState
		d.muCapture.Unlock()

		switch desired {
		case streamOff:
			if d.stream != nil {
				d.closeStream()
			}
		case streamWarm:
			if err := d.openStream(); err != nil {
				d.muCapture.Lock()
				if d.desiredStreamState != streamWarm {
					d.muCapture.Unlock()
					continue
				}
				d.desiredStreamState = streamOff
				d.muCapture.Unlock()

				d.notifyErr(err, "audio-capture", "failed to open audio capture to stay warm")
				continue
			}

			if err := d.stream.Stop(); err != nil {
				d.muCapture.Lock()
				if d.desiredStreamState != streamWarm {
					d.muCapture.Unlock()
					continue
				}
				d.desiredStreamState = streamOff
				d.muCapture.Unlock()

				d.closeStream()
				d.notifyErr(err, "audio-capture", "failed to stop audio capture and stay warm")
			}
		case streamRecording:
			if err := d.openStream(); err != nil {
				d.muCapture.Lock()
				if d.desiredStreamState != streamRecording {
					d.muCapture.Unlock()
					continue
				}
				clipID := d.active.ID()
				d.cancelPendingStop()
				d.active = nil
				d.desiredStreamState = streamOff
				d.muCapture.Unlock()

				d.notifyErr(err, "audio-capture", "failed to open audio capture for clip %d", clipID)
				continue
			}

			err := d.stream.Start()
			if err == nil {
				continue
			}

			d.muCapture.Lock()
			if d.desiredStreamState != streamRecording {
				d.muCapture.Unlock()
				continue
			}
			clipID := d.active.ID()
			d.cancelPendingStop()
			d.active = nil
			d.desiredStreamState = streamOff
			d.muCapture.Unlock()

			d.closeStream()
			d.notifyErr(err, "audio-capture", "failed to start audio capture for clip %d", clipID)
		}
	}
}
