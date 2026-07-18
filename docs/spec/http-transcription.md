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
Content-Type: audio/wav

<WAV body>
```

The request body is the WAV file itself, not multipart form data. The MVP accepts the same audio format produced by local capture:

1. PCM audio.
2. Signed 16-bit little-endian samples.
3. One channel.
4. 16 kHz sample rate.

The daemon should parse the WAV container and locate its format and data chunks rather than assuming a fixed-size header. Malformed WAV data and unsupported audio formats should be rejected without allocating a clip ID or invoking the output command.

The daemon should limit the request body according to the configured maximum clip duration. A body that exceeds the permitted size should be rejected without reading an unbounded amount of data into memory.

## Accepted Clip Lifecycle

After validating a request, the daemon should:

1. Allocate the next daemon-local clip ID.
2. Classify the clip using its decoded PCM duration and the configured minimum duration.
3. For a usable clip, send the audio to the configured Whisper endpoint with the current transcription prompt and request settings.
4. Process `short`, `blank`, and `text` results through the existing transcript-file and output-command behavior.
5. Apply the existing notification, logging, retention, and shutdown rules where relevant.

Uploaded WAV data should remain in memory during normal processing and should not be written to a temporary audio file.

Accepting an HTTP clip must not start, stop, replace, or warm the local microphone stream. Local capture and HTTP submission share the daemon-local clip sequence.

## Response

After the body has been validated and the clip has been accepted for processing, the endpoint should return `202 Accepted` with a JSON response containing the allocated clip ID:

```json
{"ok":true,"clip_id":42}
```

The response acknowledges acceptance only. It does not guarantee that Whisper transcription or output processing has completed successfully. Later processing failures use the daemon's existing logs and notifications.

Invalid methods, content types, bodies, and audio formats should receive an appropriate unsuccessful HTTP response with a small JSON error body. Exact status-code and error-text mappings are implementation details except where specified here.

## Sequential Processing

The MVP should admit and process HTTP transcription requests sequentially. At most one HTTP submission may be validated or processed at a time. A later request should wait until the current HTTP submission has completed transcription and output processing, or has failed.

This serialization prevents parallel mobile submissions and retries from creating unbounded in-memory audio or concurrent editor insertions. It applies to HTTP submissions; it does not change the existing concurrency behavior of clips captured through the local microphone.

## Future Extensions

Possible later extensions include:

1. Authentication and transport security.
2. Additional audio formats or server-side resampling.
3. Explicit queue limits, concurrency controls, and busy responses.
4. Idempotency keys to make mobile retries safe.
5. A way for HTTP clients to query the final transcription or output result.
