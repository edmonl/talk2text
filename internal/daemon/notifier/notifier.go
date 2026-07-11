// Package notifier dispatches daemon notifications through an external command.
package notifier

import (
	"context"
	"log"
	"os/exec"
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

// Info emits an informational notification.
func (n *Notifier) Info(code, message string) {
	n.emit("info", code, message)
}

// Error emits an error notification.
func (n *Notifier) Error(code, message string) {
	n.emit("error", code, message)
}

func (n *Notifier) emit(level, code, message string) {
	if n.cmd == "" {
		return
	}
	go func() {
		cmd := exec.CommandContext(n.ctx, n.cmd, level, code, message)
		cmd.Stderr = n.log.Writer()
		if err := cmd.Start(); err != nil {
			n.log.Printf("notification command start failed: %s", err)
			return
		}
		cmd.Wait()
	}()
}
