# 0004. Use an external output command

## Status
Accepted

## Context
Transcription output can go to different destinations: clipboard, a popup Neovim window, or an already-running Neovim instance. These targets are desktop- and editor-specific, and they are often easier to express as shell scripts than as Go code.

Keeping targets inside the Go daemon would couple the daemon to optional tools and libraries. It would also make user-defined output behavior require Go changes or rebuilds.

Neovim integration also needs dynamic switching. For example, when focus moves between two Neovim instances, the active output destination may change while the daemon continues running.

## Decision
Use an external output command contract.

The daemon accepts an `--output-cmd` path. After transcription succeeds, it writes the cleaned transcript text to a per-clip transcript file and invokes:

```sh
<output-cmd> <clip_transcript_file>
```

The output command path may be a symlink. External tools may atomically update that symlink to point at a different output command.

The daemon resolves and captures the output command at recording start. If the symlink changes during a recording, the active recording still uses the command captured at start; the new command applies to the next recording.

## Consequences
The Go daemon stays focused on recording, transcription, IPC, and process orchestration. Clipboard, Sway, terminal, and Neovim behavior can live in small scripts.

Users can customize output behavior by writing any executable that accepts the per-clip transcript file path as its first argument.

The daemon needs robust process execution behavior: timeout handling, stderr logging, executable checks, and clear failure notifications. Failed output should leave the per-clip transcript file available for inspection.

The symlink-based switching model is simple and works for focus-driven editor integrations, but it does not provide structured metadata such as display names or availability. A future descriptor format can be introduced if that becomes necessary.
