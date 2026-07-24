# 0007. Add a daemon-owned recording stop delay

## Status

Accepted

## Context

Users can release the push-to-talk shortcut slightly before finishing their final syllable. Stopping capture at the release event clips the end of the utterance.

Delaying the client command is unsafe. If the user starts another recording before the delayed command is sent, the stale stop can stop the new clip. Delaying command acknowledgement would also couple shortcut latency to recording work, contrary to [ADR 0006](0006-use-acknowledgement-only-start-and-stop-responses.md).

## Decision

The daemon acknowledges an accepted stop command immediately and continues capturing the active clip for a configurable stop delay. `TALK2TEXT_STOP_DELAY` controls the delay and defaults to `250ms`; `0s` restores immediate stopping.

The pending stop is associated with the active clip ID. Repeated stop commands do not extend the deadline. Maximum-duration stop, capture failure, and shutdown cancel the pending timer.

If a new start arrives during the delay, the daemon finalizes and processes the pending clip immediately, cancels its timer, and starts a new clip. The timer checks the clip ID before stopping so it cannot affect a later clip.

## Consequences

The end of speech is less likely to be clipped, at the cost of the configured amount of additional transcription latency and trailing ambient audio.

Status continues to report the session as active during the short delay. Warm retention begins after capture actually stops. Borderline recordings may accumulate enough trailing audio to meet the configured minimum duration and become eligible for transcription.
