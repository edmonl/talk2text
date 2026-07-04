# 0004. Use an external output command

## Status
Accepted

## Context
Transcription output can go to different destinations: clipboard, a popup Neovim window, or an already-running Neovim instance. These targets are desktop- and editor-specific, and they are often easier to express as shell scripts than as Go code.

Keeping targets inside the Go daemon would couple the daemon to optional tools and libraries. It would also make user-defined output behavior require Go changes or rebuilds.

Some integration may need dynamic destination switching. For example, when focus moves between two Neovim instances, an external integration may need to change where future transcripts are sent while the daemon continues running.

## Decision
Use an external output command contract.

The daemon accepts a configured output command. After clip processing completes, it writes the output text to a per-clip transcript file and invokes the command with enough arguments for the command to know both the clip classification and transcript file path.

The configured output command is invariant from the daemon's perspective. The daemon does not choose destinations, track focus, or manage routing state.

External systems may implement routing behind that command. For example, the configured command may be a stable wrapper script or symlink whose target is managed outside this project.

## Consequences
The Go daemon stays focused on recording, transcription, IPC, transcript file creation, and process orchestration. Clipboard, Sway, terminal, and Neovim behavior can live in small scripts.

Users can customize output behavior by writing any executable that follows the output command contract defined in the current spec.

The daemon needs a small, stable process contract for handing completed transcripts to user-managed tools.

Routing behavior belongs to the external output command or the system that manages it. This keeps destination-specific state outside the daemon.
