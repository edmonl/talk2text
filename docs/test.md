# Manual Smoke Tests

The automated Go suite covers protocol handling, error paths, concurrency, retention, runtime-path safety, and shutdown behavior. This checklist is limited to integrations that require real hardware or the intended network environment.

Assume commands run from the project root and the actual Whisper-compatible server is already available.

## Microphone and Whisper

Goal: confirm real microphone capture reaches the configured Whisper server and produces a transcript.

Steps:

1. Build `talk2text`.
2. Start the daemon in a terminal with a temporary runtime directory and the actual Whisper endpoint.
3. In another terminal, run `start`, speak a short phrase, and run `stop` using the same runtime directory.
4. Confirm the daemon reports successful transcription and the transcript file contains the expected text.

Affected state: microphone access, the Whisper server, the daemon process, and files under the temporary runtime directory.

Cleanup: stop the daemon and remove the temporary runtime directory.

## Remote HTTP Submission

Goal: confirm the intended mobile device can submit AMR-WB audio over the real private network or tunnel.

Steps:

1. Start the daemon with `--http-listen` bound to the intended trusted-network address and a temporary runtime directory.
2. Capture or select a valid single-channel AMR-WB storage-format recording on the remote device.
3. Submit it to `POST /transcribe` with `Content-Type: audio/amr-wb`.
4. Confirm the response is `202 Accepted` with a clip ID.
5. Confirm the corresponding transcript is produced through the configured Whisper and output pipeline.

Affected state: the trusted network or tunnel, HTTP listener, Whisper server, daemon process, and files under the temporary runtime directory.

Cleanup: stop the daemon and remove the temporary runtime directory.

Risk: the HTTP listener has no authentication or TLS. Do not expose it directly to an untrusted network.
