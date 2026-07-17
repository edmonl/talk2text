# 0007. Make output commands responsible for transcript cleanup

Date: 2026-07-17

## Status
Accepted

## Context
The daemon writes each completed transcript to a file and invokes the configured output command with that file's path. The previous contract made the daemon remove the file when the invoked process exited successfully.

An output command may delegate processing to one or more other processes and exit before they finish reading the transcript. Its exit status therefore cannot reliably tell the daemon that transcript processing is complete. Requiring the final processing result to propagate back through every process would complicate output integrations.

## Decision
The output command is responsible for removing a transcript file after it successfully processes that transcript. If it delegates processing, it must arrange for the file to be removed only after the delegated processing succeeds.

The daemon does not treat the output command's exit status as proof that processing is complete and does not remove the transcript file when the command exits. It may still use the exit status for output-failure reporting. Files left behind are removed by the existing stale-transcript cleanup when the daemon next starts.

## Consequences
Output commands can safely use asynchronous or multi-process workflows without propagating a final processing result back to the daemon.

Transcript files remain available until the process that actually consumes them finishes.

Each output command must implement successful cleanup. A missing or faulty cleanup leaves transcript files, including potentially sensitive text, in the runtime directory until the next daemon startup.
