// Package timer provides small start/stop timer abstractions.
package timer

import (
	"time"
)

// Timer starts and stops a scheduled action.
type Timer interface {
	Start()
	Stop()
}

// ImmediateTimer invokes a callback every time it is started.
type ImmediateTimer struct {
	fn func()
}

// NewImmediateTimer returns a timer that invokes fn immediately on Start.
func NewImmediateTimer(fn func()) Timer {
	return ImmediateTimer{fn: fn}
}

// Start invokes the timer callback immediately.
func (t ImmediateTimer) Start() {
	t.fn()
}

// Stop does nothing.
func (ImmediateTimer) Stop() {}

// NewCallbackTimer returns a stopped timer that invokes fn after duration.
// Non-positive durations return an immediate timer.
func NewCallbackTimer(duration time.Duration, fn func()) Timer {
	if duration <= 0 {
		return NewImmediateTimer(fn)
	}
	t := time.AfterFunc(duration, fn)
	t.Stop()
	return CallbackTimer{
		duration: duration,
		timer:    t,
	}
}

// CallbackTimer invokes a callback after its configured duration.
type CallbackTimer struct {
	duration time.Duration
	timer    *time.Timer
}

// Start schedules the callback.
func (t CallbackTimer) Start() {
	t.timer.Reset(t.duration)
}

// Stop cancels the callback if it has not run yet.
func (t CallbackTimer) Stop() {
	t.timer.Stop()
}
