package dgo

import (
	"context"
	"sync"
	"testing"
	"time"
)

type gatewayWriteFixture struct {
	mu    sync.Mutex
	clock *gatewayWriteFixtureClock
	times []time.Time
	data  []interface{}
}

type gatewayWriteFixtureClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (clock *gatewayWriteFixtureClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *gatewayWriteFixtureClock) Wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clock.mu.Lock()
	clock.waits = append(clock.waits, delay)
	clock.now = clock.now.Add(delay)
	clock.mu.Unlock()
	return ctx.Err()
}

func (clock *gatewayWriteFixtureClock) Waits() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration(nil), clock.waits...)
}

func (fixture *gatewayWriteFixture) WriteJSON(data interface{}) error {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	fixture.times = append(fixture.times, fixture.clock.Now())
	fixture.data = append(fixture.data, data)
	return nil
}

func (fixture *gatewayWriteFixture) snapshot() ([]time.Time, []interface{}) {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return append([]time.Time(nil), fixture.times...), append([]interface{}(nil), fixture.data...)
}

func TestGatewayOutboundRateLimiterFixture(t *testing.T) {
	clock := &gatewayWriteFixtureClock{now: time.Unix(700, 0)}
	fixture := &gatewayWriteFixture{clock: clock}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := newGatewayWriteQueue(ctx, fixture, &sync.Mutex{}, clock)
	go queue.run()

	for event := 0; event < gatewaySendLimit; event++ {
		if err := queue.enqueue(ctx, map[string]int{"event": event}); err != nil {
			t.Fatalf("enqueue event %d: %v", event, err)
		}
	}
	if waits := clock.Waits(); len(waits) != 0 {
		t.Fatalf("rate limiter waited before reaching the limit: %v", waits)
	}
	if err := queue.enqueue(ctx, map[string]int{"event": gatewaySendLimit}); err != nil {
		t.Fatalf("enqueue event %d: %v", gatewaySendLimit, err)
	}
	times, data := fixture.snapshot()
	if len(data) != gatewaySendLimit+1 {
		t.Fatalf("fixture received %d events, want %d", len(data), gatewaySendLimit+1)
	}
	if waits := clock.Waits(); len(waits) != 1 || waits[0] != gatewaySendWindow {
		t.Fatalf("rate limiter waits = %v, want [%s]", waits, gatewaySendWindow)
	}
	if got := times[gatewaySendLimit].Sub(times[0]); got != gatewaySendWindow {
		t.Fatalf("event %d sent after %s, want %s", gatewaySendLimit, got, gatewaySendWindow)
	}

	cancel()
	select {
	case <-queue.done:
	case <-time.After(time.Second):
		t.Fatal("gateway write queue did not stop after context cancellation")
	}
}
