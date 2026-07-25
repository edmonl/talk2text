# HTTP Audio Submission

This document specifies a future HTTP input for `talk2text`. It is not part of the current daemon behavior.

## Goal

Allow a mobile device to capture audio and submit it to the `talk2text` daemon running on a remote machine where the user is editing through SSH. The submitted audio should enter the same transcription and output pipeline as a clip captured by the daemon's local microphone.

```text
mobile capture -> POST /transcribe -> Whisper -> transcript file -> output command
```

The HTTP input supplements local microphone capture. It does not replace the existing Unix-socket commands or take control of an active local recording.

## Availability

The HTTP listener should be optional and disabled by default. The user must explicitly configure a listen address when starting the daemon.

The MVP does not provide HTTP authentication or TLS. It is intended to listen only on a trusted private network address or behind a user-managed encrypted tunnel. It should not be exposed directly to an untrusted network. Authentication and transport security remain future work.

## Request

The daemon should accept:

```http
POST /transcribe
Content-Type: audio/amr-wb

<AMR-WB body>
```

The request body is the encoded audio itself, not multipart form data.

The request must contain an AMR-WB storage-format stream. The daemon should decode it to signed 16-bit, one-channel, 16 kHz PCM before applying duration rules or sending the audio to Whisper.

Malformed AMR-WB data and unsupported audio formats should be rejected without allocating a clip ID or invoking the output command.

The daemon should limit the encoded request body according to the configured maximum clip duration and should also reject decoded audio that exceeds that duration. A body that exceeds the permitted size should be rejected without reading an unbounded amount of data into memory.

## AMR-WB Decoder

The implementation should vendor only the AMR-WB decoder sources from a pinned upstream OpenCORE AMR release.

A small internal cgo wrapper should expose decoder initialization, single-frame decoding, and cleanup. Go code should validate the AMR-WB storage-format header and frames, manage decoder lifetime, and collect the decoded PCM.

Vendored upstream files should remain isolated from project-owned code. Their license and attribution notices must be preserved, and any local modifications should be kept minimal and clearly identified.

## Accepted Clip Lifecycle

After validating a request, the daemon should:

1. Allocate the next daemon-local clip ID.
2. Classify the clip using its decoded PCM duration and the configured minimum duration.
3. For a usable clip, send the audio to the configured Whisper endpoint with the current transcription prompt and request settings.
4. Process `short`, `blank`, and `text` results through the existing transcript-file and output-command behavior.
5. Apply the existing notification, logging, retention, and shutdown rules where relevant.

Uploaded audio and any decoded PCM should remain in memory during normal processing and should not be written to a temporary audio file.

Accepting an HTTP clip must not start, stop, replace, or warm the local microphone stream. Local capture and HTTP submission share the daemon-local clip sequence.

## Response

After the body has been validated and the clip has been accepted for processing, the endpoint should return `202 Accepted` with a JSON response containing the allocated clip ID:

```json
{"ok":true,"clip_id":42}
```

The response acknowledges acceptance only. It does not guarantee that Whisper transcription or output processing has completed successfully. Later processing failures use the daemon's existing logs and notifications.

Invalid methods, content types, bodies, and audio formats should receive an appropriate unsuccessful HTTP response with a small JSON error body. Exact status-code and error-text mappings are implementation details except where specified here.

## Concurrency

The daemon should maintain a counter of admitted HTTP requests that have not started transcription. On receiving a `POST /transcribe` request, it should atomically increment the counter. If the resulting value is greater than `2`, it should immediately decrement the counter and return `503 Service Unavailable` with a small JSON busy response without decoding the body.

An admitted request should remain counted while its body is being read, validated, or decoded and while it is waiting to start transcription. A request that fails or finishes without transcription must decrement the counter. A usable request should decrement the counter immediately before sending its decoded audio to Whisper.

HTTP requests may be read, validated, and decoded concurrently, but their Whisper requests should start and run one at a time. While one HTTP request is being transcribed, up to two more may be handled or wait for transcription. Further HTTP requests should be rejected as busy.

Before starting its Whisper request, an HTTP submission should wait while any transcription from local microphone capture is already in flight. This is not an exclusive global transcription lock: a local transcription that starts after the HTTP transcription has begun may run concurrently with it. Output processing is not part of the HTTP transcription serialization and follows the existing concurrency behavior.

## Future Extensions

Possible later extensions include:

1. Authentication and transport security.
2. Further audio formats or server-side resampling.
3. Configurable HTTP admission limits and transcription priority or fairness controls.
4. Idempotency keys to make mobile retries safe.
5. A way for HTTP clients to query the final transcription or output result.
