# 0001. Use malgo for audio capture

## Status
Accepted

## Context
The daemon needs to capture microphone audio directly from Go so the shortcut path does not spawn `ffmpeg` for each recording. The capture layer should work well on Linux desktops, support low-latency capture, and keep installation reasonably simple.

The main alternatives considered were:

1. `github.com/gen2brain/malgo`, a Go binding for `miniaudio`.
2. `github.com/gordonklaus/portaudio`, a Go binding for PortAudio.
3. Continuing to shell out to `ffmpeg`.

## Decision
Use `github.com/gen2brain/malgo` for the initial Go daemon audio capture implementation.

## Consequences
`malgo` lets the daemon request signed 16-bit PCM, mono, 16 kHz capture directly and supports Linux audio backends through `miniaudio`.

The project will use cgo for the daemon. If `malgo` proves unreliable on the target systems, a later ADR can supersede this decision with another backend or a native audio integration.

PortAudio remains a viable fallback, but it has a heavier system dependency story. Spawning `ffmpeg` remains useful for debugging but is not the normal runtime path.
