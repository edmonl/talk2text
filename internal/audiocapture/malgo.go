package audiocapture

import (
	"fmt"
	"unsafe"

	"github.com/edmonl/talk2text/internal/whisper"
	"github.com/gen2brain/malgo"
)

type malgoStream struct {
	ctx    *malgo.AllocatedContext
	device *malgo.Device
}

// NewMalgoStream opens a malgo capture stream.
func NewMalgoStream(cfg Config) (Stream, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate context: %w", err)
	}
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = 1
	deviceConfig.SampleRate = whisper.SampleRateHz
	deviceConfig.Alsa.NoMMap = 1
	deviceConfig.Pulse.StreamNameCapture = "talk2text"
	var selectedDevice malgo.DeviceID
	if cfg.InputDevice != "" {
		devices, dErr := ctx.Devices(malgo.Capture)
		if dErr != nil {
			ctx.Uninit()
			ctx.Free()
			return nil, fmt.Errorf("failed to list capture devices: %w", dErr)
		}
		found := false
		for _, device := range devices {
			if device.Name() == cfg.InputDevice || device.ID.String() == cfg.InputDevice {
				selectedDevice = device.ID
				found = true
				break
			}
		}
		if !found {
			ctx.Uninit()
			ctx.Free()
			return nil, fmt.Errorf("failed to find device %s", cfg.InputDevice)
		}
		// miniaudio copies pDeviceID during ma_device_init, so this local only
		// needs to remain valid until malgo.InitDevice returns.
		deviceConfig.Capture.DeviceID = unsafe.Pointer(&selectedDevice)
	}
	callbacks := malgo.DeviceCallbacks{
		Data: func(_, input []byte, _ uint32) {
			if len(input) > 0 {
				cfg.OnPCM(input)
			}
		},
	}
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, callbacks)
	if err != nil {
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("failed to initialize device: %w", err)
	}
	rate := device.SampleRate()
	if rate != whisper.SampleRateHz {
		device.Uninit()
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("audio backend did not provide requested %d Hz capture but %d Hz", whisper.SampleRateHz, rate)
	}
	if device.CaptureFormat() != malgo.FormatS16 || device.CaptureChannels() != 1 {
		device.Uninit()
		ctx.Uninit()
		ctx.Free()
		return nil, fmt.Errorf("audio backend did not provide requested signed 16-bit mono capture")
	}
	return &malgoStream{ctx: ctx, device: device}, nil
}

func (s *malgoStream) Start() error {
	// This is idempotent and returns no error if the device has been started.
	return s.device.Start()
}

func (s *malgoStream) Stop() error {
	// Stopping a stopped device does not return an error.
	// This is idempotent as long as the device stays initialized.
	return s.device.Stop()
}

func (s *malgoStream) Close() error {
	// Uninit is NOT idempotent but it stops the device if it has not been stopped.
	s.device.Uninit()
	err := s.ctx.Uninit()
	s.ctx.Free()
	return err
}
