# 0006. Use acknowledgement-only start and stop responses

## Status
Accepted

## Context
The primary `start` and `stop` clients are shortcut-facing commands. They should connect to the daemon, send one command, receive a small response, and exit without waiting for recording work to complete.

Starting or stopping recording can involve session state changes, audio stream operations, warm-retention timer updates, notifications, and later transcription or output work. Some of those operations may block or take longer than a shortcut client should wait.

If the IPC response means "the daemon has completed the requested recording operation", client latency becomes coupled to the slowest part of the daemon's recording path. That makes key press and key release shortcuts more fragile, especially when an audio backend is slow.

## Decision
Successful `start` and `stop` IPC responses acknowledge that the daemon received and accepted the request. They do not mean the daemon has already completed the recording-state transition, audio stream operation, notification, transcription, or output work associated with the command.

The daemon remains responsible for handling accepted recording commands according to the current spec. This decision only defines what the client may infer from the response. It does not require a particular internal scheduling, queuing, or serialization design.

`status` remains the command for observing daemon state after command handling has progressed.

## Consequences
Shortcut clients can return promptly after the daemon accepts a command.

The IPC contract does not require successful `start` and `stop` requests to expose audio backend latency as client latency.

Clients cannot infer from a successful `start` or `stop` response that recording has already started or stopped. Clients that need current state should call `status`.
