package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// liveHub tells the live-score streams watching a match that it changed.
// A signal carries no data: each stream reloads the match and sends it, so a
// missed or doubled signal can never leave a viewer with a wrong score.
type liveHub struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
	// done closes on shutdown, ending every stream so a graceful shutdown
	// doesn't wait on them.
	done     chan struct{}
	shutOnce sync.Once
}

func newLiveHub() *liveHub {
	return &liveHub{subs: make(map[string]map[chan struct{}]struct{}), done: make(chan struct{})}
}

// shutdown ends every open stream. Safe to call more than once.
func (h *liveHub) shutdown() {
	h.shutOnce.Do(func() { close(h.done) })
}

// subscribe returns a channel signalled when the match changes, and a
// function to stop.
func (h *liveHub) subscribe(matchID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[matchID] == nil {
		h.subs[matchID] = make(map[chan struct{}]struct{})
	}
	h.subs[matchID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[matchID], ch)
		if len(h.subs[matchID]) == 0 {
			delete(h.subs, matchID)
		}
		h.mu.Unlock()
	}
}

// publish signals every stream watching the match. It never blocks: a
// stream that hasn't caught up already has a signal waiting.
func (h *liveHub) publish(matchID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[matchID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// startLiveListener runs listenLive and returns a function that stops it and
// waits until its connection is back in the pool. Call it before closing the
// pool: pgxpool.Close waits for every borrowed connection, and the listener
// otherwise holds one until its context ends, so closing the pool first would
// hang (for instance when the server can't bind its port).
func startLiveListener(ctx context.Context, pool *pgxpool.Pool, hub *liveHub) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		listenLive(ctx, pool, hub)
	}()
	return func() {
		cancel()
		<-done
	}
}

// listenLive relays the database's match_live notifications to the hub, so
// changes made through any server instance reach this one's viewers. It
// reconnects until ctx is done.
func listenLive(ctx context.Context, pool *pgxpool.Pool, hub *liveHub) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := func() error {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				return err
			}
			defer conn.Release()
			if _, err := conn.Exec(ctx, "LISTEN "+liveChannel); err != nil {
				return err
			}
			backoff = time.Second
			for {
				n, err := conn.Conn().WaitForNotification(ctx)
				if err != nil {
					return err
				}
				hub.publish(n.Payload)
			}
		}()
		if ctx.Err() != nil {
			return
		}
		log.Printf("live scores: listener stopped (%v); retrying in %s", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}
