package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/edmonl/talk2text/cmd/talk2text/flags"
	"github.com/edmonl/talk2text/internal/client"
	"github.com/edmonl/talk2text/internal/daemon"
	"github.com/edmonl/talk2text/internal/daemon/config"
)

const (
	exitOk = iota
	exitFailure
	exitUsage
	exitUnavailable
)

func main() {
	exitCode := exitFailure
	err := run(os.Args[1:], os.Stdout, os.Stderr, &exitCode)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintf(os.Stderr, "talk2text: %s\n", err)
		}
		os.Exit(exitCode)
	}
}

func parseArgs(args []string, cliFlags *flags.Flags, exitCode *int) error {
	fs, err := cliFlags.Parse(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			*exitCode = exitOk
			return flag.ErrHelp
		}
		*exitCode = exitUsage
		return err
	}
	if fs.NArg() != 0 {
		*exitCode = exitUsage
		return fmt.Errorf("unknown argument %s", fs.Arg(0))
	}

	return nil
}

func run(args []string, stdout, stderr io.Writer, exitCode *int) error {
	if len(args) == 0 {
		flags.NewGlobalFlags(stdout).Usage()
		return nil
	}

	command := args[0]
	switch command {
	case "daemon":
		cfg, err := config.DefaultConfig()
		if err != nil {
			return err
		}

		err = parseArgs(args[1:], flags.NewDaemonFlags(&cfg, stdout), exitCode)
		if err != nil {
			return err
		}

		cfg.RuntimeDir, err = resolveRuntimeDir(cfg.RuntimeDir)
		if err != nil {
			return err
		}

		err = config.ValidateConfig(cfg)
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return daemon.Run(ctx, cfg, stderr)
	case "start", "stop", "status":
		var runtimeDir string
		err := parseArgs(args[1:], flags.NewClientFlags(command, &runtimeDir, stdout), exitCode)
		if err != nil {
			return err
		}

		runtimeDir, err = resolveRuntimeDir(runtimeDir)
		if err != nil {
			return err
		}

		err = client.Run(command, runtimeDir, stdout)
		if errors.Is(err, client.ErrDaemonUnavailable) {
			*exitCode = exitUnavailable
		}

		return err
	}

	return parseArgs(args, flags.NewGlobalFlags(stdout), exitCode)
}

func resolveRuntimeDir(runtimeDir string) (string, error) {
	if runtimeDir != "" {
		return runtimeDir, nil
	}

	envName := "XDG_RUNTIME_DIR"
	path := os.Getenv(envName)
	userDirName := ""
	if path == "" {
		envName = "TMPDIR"
		path = os.Getenv(envName)
		userDirName = "run-" + strconv.Itoa(os.Getuid())
	}

	if path == "" {
		envName = "/tmp"
		path = "/tmp"
	} else if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s must be absolute", envName)
	}

	if info, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%s must be an existing directory", envName)
		}
		return "", fmt.Errorf("failed to inspect %s: %w", envName, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("%s must be a directory", envName)
	}

	return filepath.Join(path, userDirName, "talk2text"), nil
}
