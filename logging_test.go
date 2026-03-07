package dgo

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestSessionLog_NoDeadlockUnderWriteLock(t *testing.T) {
	s := &Session{}
	s.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	done := make(chan struct{})
	go func() {
		s.Lock()
		defer s.Unlock()
		s.log(LogInformational, "locked log call")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Session.log deadlocked while session write lock was held")
	}
}
