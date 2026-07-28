// Package notifier dispatches daemon notifications through an external command.
package notifier

import (
	"context"
	"log"
	"os"
	"os/exec"
)

const (
	notifyLevelEnv = "TALK2TEXT_NOTIFY_LEVEL"
	notifyCodeEnv  = "TALK2TEXT_NOTIFY_CODE"
)

// Notifier emits daemon notifications.
type Notifier struct {
	ctx context.Context
	cmd string
	log *log.Logger
}

// New returns a notifier that invokes cmd for each notification.
func New(ctx context.Context, cmd string, logger *log.Logger) *Notifier {
	return &Notifier{ctx: ctx, cmd: cmd, log: logger}
}

// Info emits an informational notification with a request environment.
func (n *Notifier) Info(code, message string, environment []string) {
	n.emit("info", code, message, environment)
}

// Error emits an error notification with a request environment.
func (n *Notifier) Error(code, message string, environment []string) {
	n.emit("error", code, message, environment)
}

func (n *Notifier) emit(level, code, message string, environment []string) {
	if n.cmd == "" {
		return
	}
	// Append in precedence order: daemon environment, captured request
	// environment, then daemon-owned metadata.
	commandEnvironment := append(os.Environ(), environment...)
	commandEnvironment = append(commandEnvironment,
		notifyLevelEnv+"="+level,
		notifyCodeEnv+"="+code,
	)
	go func() {
		cmd := exec.CommandContext(n.ctx, n.cmd, message)
		cmd.Env = commandEnvironment
		cmd.Stderr = n.log.Writer()
		if err := cmd.Start(); err != nil {
			n.log.Printf("notification command start failed: %v", err)
			return
		}
		// Wait only reaps the process; notification failures are intentionally
		// non-fatal after startup.
		cmd.Wait()
	}()
}
