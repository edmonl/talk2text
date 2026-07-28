# Transcript File Retention

`TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW` limits runtime transcript accumulation by retaining a sliding window of recent eligible clip IDs. The default is `100`; `0` disables runtime retention. Invalid or negative values are configuration errors.

A transcript is protected while its output command may still need it. Once the command exits or fails to start, the transcript becomes eligible for cleanup. Without an output command, it is eligible after being written. Protected files may temporarily exceed the configured window.

Cleanup removes unprotected regular transcript files older than the current window. It ignores unrelated names, directories, and symbolic links, never follows paths outside the transcript directory, and does not fail clip processing because one file cannot be removed.

Output commands that delegate work remain responsible for consuming and removing transcript files before exiting. The daemon does not use command success as evidence that delegated processing finished.
