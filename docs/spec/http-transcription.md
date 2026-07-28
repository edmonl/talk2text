# HTTP Audio Submission

The optional HTTP input lets trusted remote devices submit audio to the same transcription and output pipeline as local recordings. It does not control or replace local microphone capture.

## Availability and Security

`--http-listen <address>` enables the listener; it is disabled by default. HTTP input requires a nonzero `TALK2TEXT_MAX_DURATION`.

The listener provides neither authentication nor TLS. It must be limited to a trusted private network or placed behind a user-managed encrypted tunnel.

## Request

```http
POST /transcribe
Content-Type: audio/amr-wb

<AMR-WB storage-format body>
```

The body is encoded audio, not multipart data. Optional routing environment values use the `Talk2Text-Env` header specified in [Request Environment and Output Routing](request-environment.md).

The daemon decodes valid AMR-WB into signed 16-bit mono PCM at 16 kHz. Encoded and decoded audio are bounded by the configured maximum duration and remain in memory. Malformed, unsupported, or oversized input is rejected without allocating a clip ID.

The AMR-WB decoder is vendored from OpenCORE AMR `0.1.6`. Upstream license and attribution notices must be preserved.

## Accepted Clips

An accepted submission:

1. Receives the next daemon-local clip ID.
2. Uses the same `short`, `blank`, and `text` classification as local capture.
3. Uses the shared Whisper, output, notification, logging, retention, and shutdown behavior.
4. Does not start, stop, replace, or warm the local microphone.

The endpoint returns `202 Accepted` after validation and admission:

```json
{"ok":true,"clip_id":42}
```

This acknowledges acceptance, not completion. Invalid requests receive an unsuccessful HTTP response with a small JSON error body.

## Concurrency

At most two HTTP requests may be admitted but not yet transcribing; additional requests receive `503 Service Unavailable`.

HTTP bodies may be validated concurrently, but HTTP Whisper requests run one at a time. An HTTP transcription waits for local transcriptions already in flight. Local transcription may start while an HTTP transcription is running, and output processing is not serialized with HTTP transcription.
