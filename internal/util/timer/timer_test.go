package timer

import (
	"testing"
	"time"
)

func TestCallbackTimerStartsStopped(t *testing.T) {
	fired := make(chan struct{}, 1)
	timer := NewCallbackTimer(20*time.Millisecond, func() {
		fired <- struct{}{}
	})

	select {
	case <-fired:
		t.Fatal("timer fired before Start")
	case <-time.After(40 * time.Millisecond):
	}

	timer.Start()
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("timer did not fire after Start")
	}
}

func TestCallbackTimerStopCancelsCallback(t *testing.T) {
	fired := make(chan struct{}, 1)
	timer := NewCallbackTimer(40*time.Millisecond, func() {
		fired <- struct{}{}
	})

	timer.Start()
	timer.Stop()

	select {
	case <-fired:
		t.Fatal("timer fired after Stop")
	case <-time.After(80 * time.Millisecond):
	}
}

func TestNonPositiveCallbackTimerFiresImmediately(t *testing.T) {
	fired := make(chan struct{}, 1)
	timer := NewCallbackTimer(0, func() {
		fired <- struct{}{}
	})
	timer.Start()

	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("timer did not fire immediately")
	}
}
