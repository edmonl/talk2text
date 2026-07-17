# Transcript File Retention

The daemon retains transcript files for a sliding window of recent clip IDs so failed output processing or cleanup cannot cause unbounded growth of the transcripts directory.

`TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW` configures the number of recent clip IDs in the window. The default is `100`. A value of `0` disables runtime retention cleanup. Negative values and values that are not integers are configuration errors.

The daemon tracks the greatest clip ID whose transcript is eligible for retention cleanup. A transcript becomes eligible immediately after writing when no output command is configured, or when its output command exits or fails to start. For a positive window, an eligible transcript remains in the window when its clip ID is greater than or equal to:

```text
greatest_eligible_clip_id - transcript_retention_window + 1
```

For example, while clip ID `100` is protected and the greatest eligible clip ID is `99`, a window of `10` retains transcript IDs `90` through `99` plus protected ID `100`. Missing transcript IDs create gaps and may result in fewer than ten retained files.

A transcript file is protected from retention cleanup from the time it is written until its output command exits or fails to start. Protected transcript IDs remain retained even when they fall outside the current window, so the window is a soft storage bound while output commands are running.

Retention cleanup is event-driven; the daemon does not periodically scan the transcripts directory:

1. On startup, the existing stale-transcript cleanup removes regular files left by a previous daemon run.
2. When a transcript file is written for an output command, the daemon protects its clip ID without advancing the window.
3. When an output command exits or fails to start, the daemon stops protecting that clip, advances the greatest eligible clip ID if necessary, and scans the directory against the current window.
4. When no output command is configured, the daemon advances the greatest eligible clip ID and scans immediately after writing the transcript.

Using a monotonically increasing greatest eligible clip ID prevents out-of-order transcription or output completion from moving the retention window backward.

During a retention scan, the daemon processes only regular files directly under the transcripts directory whose filenames contain a valid positive clip ID. It removes unprotected files whose clip IDs fall below the current window. Files without valid clip IDs, directories, and symbolic links are ignored by runtime retention. The daemon does not recursively remove directories or follow paths outside the transcripts directory. A file that disappears concurrently is not an error. Other individual removal failures should not fail transcription or output processing.

Once an output command exits, its transcript is eligible for retention cleanup regardless of whether the command delegated processing to another process. A delegating output command remains responsible for arranging timely consumption and removal of the transcript file. Runtime retention cleanup does not use command success as evidence that output processing completed successfully.
