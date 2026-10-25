package main

import (
	"context"
	"testing"
	"time"
)

// Regression: when the server couldn't bind its port, run() returned and the
// deferred store.Close() waited forever for the listener's connection, whose
// context was only cancelled afterwards. The pool was already closed, so the
// challenge sweeper logged "closed pool" every 30s and the process never
// exited.
func TestLiveListenerLetsThePoolClose(t *testing.T) {
	store := eventsTestStore(t)
	stop := startLiveListener(context.Background(), store.pool, newLiveHub())

	// Wait until the listener has borrowed its connection.
	deadline := time.Now().Add(5 * time.Second)
	for store.pool.Stat().AcquiredConns() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("listener never acquired a connection")
		}
		time.Sleep(10 * time.Millisecond)
	}

	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stopping the listener hung")
	}
	if n := store.pool.Stat().AcquiredConns(); n != 0 {
		t.Errorf("after stop, %d connections are still borrowed; pool.Close would hang", n)
	}
}
