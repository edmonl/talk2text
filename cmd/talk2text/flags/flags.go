// Package flags builds command-line flag sets and usage output.
package flags

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/edmonl/talk2text/internal/daemon/config"
)

// Flags wraps a flag set with its own usage printing.
type Flags struct {
	set    *flag.FlagSet
	stdout io.Writer
	usage  func(out io.Writer)
}

func NewGlobalFlags(stdout io.Writer) *Flags {
	return newFlags(stdout, func(out io.Writer) {
		fmt.Fprintln(out, "A push-to-talk speech-to-text tool.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintln(out, "  talk2text <subcommand> [subcommand flags]")
		fmt.Fprintln(out, "  talk2text -h/-help")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Subcommands:")
		fmt.Fprintln(out, "  daemon  run background daemon")
		fmt.Fprintln(out, "  start   start recording")
		fmt.Fprintln(out, "  stop    stop recording")
		fmt.Fprintln(out, "  status  print daemon state and configuration")
		fmt.Fprintln(out, "Use -h/-help with subcommands to print their usage.")
	})
}

func NewClientFlags(command string, runtimeDir *string, stdout io.Writer) *Flags {
	fs := newFlags(stdout, func(out io.Writer) {
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintf(out, "  talk2text %s [flags]\n", command)
	})
	fs.set.StringVar(runtimeDir, "runtime-dir", "", "runtime directory containing the daemon socket")
	return fs
}

func NewDaemonFlags(cfg *config.Config, stdout io.Writer) *Flags {
	fs := newFlags(stdout, func(out io.Writer) {
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintln(out, "  talk2text daemon [flags]")
	})
	fs.set.StringVar(&cfg.RuntimeDir, "runtime-dir", "", "runtime directory for the daemon; defaults to XDG_RUNTIME_DIR, $TMPDIR/run-<uid>, and /tmp/run-<uid>")
	fs.set.StringVar(&cfg.WhisperEndpoint, "whisper-endpoint", cfg.WhisperEndpoint, "Whisper.cpp server HTTP endpoint")
	fs.set.StringVar(&cfg.OutCmd, "out-cmd", "", "command run after each completed clip; receives clip kind and transcript path")
	fs.set.StringVar(&cfg.NotifyCmd, "notify-cmd", "", "command used to emit user notifications; receives level, event code and message")
	fs.set.StringVar(&cfg.HTTPListen, "http-listen", cfg.HTTPListen, "address for accepting AMR-WB HTTP transcription requests; disabled when empty")
	return fs
}

// Parse parses args and prints usage only for explicit -h/-help requests.
func (f *Flags) Parse(args []string) (*flag.FlagSet, error) {
	err := f.set.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		f.Usage()
	}
	return f.set, err
}

// Usage prints command usage to the configured output.
func (f *Flags) Usage() {
	f.usage(f.stdout)
	fmt.Fprintln(f.stdout)
	fmt.Fprintln(f.stdout, "Flags:")
	f.set.SetOutput(f.stdout)
	f.set.PrintDefaults()
	f.set.SetOutput(io.Discard)
	fmt.Fprintln(f.stdout, "  -h/-help")
	fmt.Fprintln(f.stdout, "        print usage")
}

func newFlags(stdout io.Writer, usage func(out io.Writer)) *Flags {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return &Flags{set: fs, stdout: stdout, usage: usage}
}
