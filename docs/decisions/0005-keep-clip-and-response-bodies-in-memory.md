# 0005. Keep clip and response bodies in memory
Date: 2026-07-04

## Status
Accepted

## Context
The daemon records short push-to-talk clips and immediately sends them to a Whisper-compatible HTTP endpoint. Earlier design notes allowed normal-operation runtime files such as `<clip_id>.wav` and `<clip_id>.response.json`.

Those files are useful for debugging, but they make the steady-state workflow more filesystem-heavy than necessary. The daemon already owns the captured audio bytes and can construct the multipart HTTP request from memory. The response body is only needed long enough to extract `.text`.

The output command still needs a file path so scripts can read the final transcript without depending on IPC or shell quoting.

## Decision
Keep recorded clip data and raw transcription responses in daemon memory during normal processing.

The daemon streams an in-memory WAV body into the Whisper-compatible HTTP request and parses the response body without writing raw response JSON to disk.

After transcription succeeds, the daemon writes the cleaned transcript to a unique per-clip transcript file and passes that file path to the configured output command.

## Consequences
Normal recording and transcription avoid persistent WAV and response JSON files.

The runtime directory stays focused on coordination files, logs, context, and per-clip transcript files needed by output commands.

Failed transcription is harder to debug because the daemon does not automatically leave the raw audio clip or response body behind. If that becomes necessary, add an explicit debug mode rather than changing the default runtime behavior.

Output commands keep a simple file-based contract while avoiding collisions between clips.
