package dgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type shardingRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip shardingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type advancingIdentifyClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (clock *advancingIdentifyClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *advancingIdentifyClock) Wait(
	ctx context.Context,
	delay time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clock.mu.Lock()
	clock.waits = append(clock.waits, delay)
	clock.now = clock.now.Add(delay)
	clock.mu.Unlock()
	return ctx.Err()
}

func (clock *advancingIdentifyClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func (clock *advancingIdentifyClock) Waits() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration(nil), clock.waits...)
}

type blockingIdentifyClock struct {
	now     time.Time
	waiting chan time.Duration
}

func (clock *blockingIdentifyClock) Now() time.Time {
	return clock.now
}

func (clock *blockingIdentifyClock) Wait(
	ctx context.Context,
	delay time.Duration,
) error {
	select {
	case clock.waiting <- delay:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

func testSessionStartLimit() SessionInformation {
	return SessionInformation{
		Total:          4,
		Remaining:      4,
		ResetAfter:     60_000,
		MaxConcurrency: 2,
	}
}

func TestIdentifyCoordinatorBucketGates(t *testing.T) {
	t.Run("same bucket waits five seconds", func(t *testing.T) {
		clock := &advancingIdentifyClock{now: time.Unix(100, 0)}
		coordinator, err := newIdentifyCoordinatorWithClock(testSessionStartLimit(), clock)
		if err != nil {
			t.Fatal(err)
		}

		if err = coordinator.Acquire(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		if err = coordinator.Acquire(context.Background(), 2); err != nil {
			t.Fatal(err)
		}

		waits := clock.Waits()
		if len(waits) != 1 || waits[0] != identifyConcurrencyWindow {
			t.Fatalf("bucket waits = %v, want [%s]", waits, identifyConcurrencyWindow)
		}
		if got := coordinator.Status().Remaining; got != 2 {
			t.Fatalf("remaining = %d, want 2", got)
		}
	})

	t.Run("different buckets start together", func(t *testing.T) {
		clock := &advancingIdentifyClock{now: time.Unix(200, 0)}
		coordinator, err := newIdentifyCoordinatorWithClock(testSessionStartLimit(), clock)
		if err != nil {
			t.Fatal(err)
		}

		if err = coordinator.Acquire(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
		if err = coordinator.Acquire(context.Background(), 1); err != nil {
			t.Fatal(err)
		}

		if waits := clock.Waits(); len(waits) != 0 {
			t.Fatalf("different concurrency buckets waited: %v", waits)
		}
		if got := coordinator.Status().Remaining; got != 2 {
			t.Fatalf("remaining = %d, want 2", got)
		}
	})
}

func TestIdentifyCoordinatorQuotaExhaustionAndReset(t *testing.T) {
	clock := &advancingIdentifyClock{now: time.Unix(300, 0)}
	limit := SessionInformation{
		Total:          3,
		Remaining:      1,
		ResetAfter:     10_000,
		MaxConcurrency: 2,
	}
	coordinator, err := newIdentifyCoordinatorWithClock(limit, clock)
	if err != nil {
		t.Fatal(err)
	}

	if err = coordinator.Acquire(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	err = coordinator.Acquire(context.Background(), 1)
	if !errors.Is(err, ErrIdentifyQuotaExhausted) {
		t.Fatalf("Acquire error = %v, want ErrIdentifyQuotaExhausted", err)
	}
	var quotaError *IdentifyQuotaExhaustedError
	if !errors.As(err, &quotaError) {
		t.Fatalf("Acquire error type = %T, want *IdentifyQuotaExhaustedError", err)
	}
	if quotaError.Total != 3 || quotaError.RetryAfter != 10*time.Second {
		t.Fatalf("quota error = %#v", quotaError)
	}

	clock.Advance(10 * time.Second)
	if err = coordinator.Acquire(context.Background(), 1); err != nil {
		t.Fatalf("Acquire after reset: %v", err)
	}
	status := coordinator.Status()
	if status.Remaining != 2 || status.ResetAfter != 10*time.Second {
		t.Fatalf("status after reset = %#v", status)
	}
}

func TestIdentifyCoordinatorCancellationDoesNotConsumeQuota(t *testing.T) {
	clock := &blockingIdentifyClock{
		now:     time.Unix(400, 0),
		waiting: make(chan time.Duration, 1),
	}
	limit := SessionInformation{
		Total:          2,
		Remaining:      2,
		ResetAfter:     60_000,
		MaxConcurrency: 1,
	}
	coordinator, err := newIdentifyCoordinatorWithClock(limit, clock)
	if err != nil {
		t.Fatal(err)
	}
	if err = coordinator.Acquire(context.Background(), 0); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- coordinator.Acquire(ctx, 1)
	}()

	select {
	case delay := <-clock.waiting:
		if delay != identifyConcurrencyWindow {
			t.Fatalf("wait delay = %s, want %s", delay, identifyConcurrencyWindow)
		}
	case <-time.After(time.Second):
		t.Fatal("Acquire did not wait for its concurrency bucket")
	}
	cancel()
	if err = <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire error = %v, want context.Canceled", err)
	}
	if got := coordinator.Status().Remaining; got != 1 {
		t.Fatalf("remaining after cancellation = %d, want 1", got)
	}
}

func TestIdentifyCoordinatorRefreshAndValidation(t *testing.T) {
	invalidLimits := []SessionInformation{
		{Total: 0, Remaining: 0, ResetAfter: 1, MaxConcurrency: 1},
		{Total: 1, Remaining: 2, ResetAfter: 1, MaxConcurrency: 1},
		{Total: 1, Remaining: 1, ResetAfter: 0, MaxConcurrency: 1},
		{Total: 1, Remaining: 1, ResetAfter: 1, MaxConcurrency: 0},
	}
	for _, limit := range invalidLimits {
		if _, err := NewIdentifyCoordinator(limit); !errors.Is(err, ErrInvalidSessionStartLimit) {
			t.Fatalf("NewIdentifyCoordinator(%#v) error = %v", limit, err)
		}
	}

	clock := &advancingIdentifyClock{now: time.Unix(500, 0)}
	coordinator, err := newIdentifyCoordinatorWithClock(testSessionStartLimit(), clock)
	if err != nil {
		t.Fatal(err)
	}
	if err = coordinator.Acquire(context.Background(), 0); err != nil {
		t.Fatal(err)
	}

	refreshed := SessionInformation{
		Total:          10,
		Remaining:      7,
		ResetAfter:     1_500,
		MaxConcurrency: 3,
	}
	if err = coordinator.Refresh(refreshed); err != nil {
		t.Fatal(err)
	}
	if got := coordinator.SessionStartLimit(); got != refreshed {
		t.Fatalf("SessionStartLimit = %#v, want %#v", got, refreshed)
	}
	if err = coordinator.Acquire(context.Background(), 0); err != nil {
		t.Fatalf("Acquire after max_concurrency refresh unexpectedly waited: %v", err)
	}
}

func TestNewShardManagerDiscoversAndCreatesRecommendedShards(t *testing.T) {
	restSession, err := New("Bot rest-token")
	if err != nil {
		t.Fatal(err)
	}
	var restCalls atomic.Int32
	restSession.Client.Transport = shardingRoundTripper(func(request *http.Request) (*http.Response, error) {
		restCalls.Add(1)
		if request.Method != http.MethodGet || request.URL.String() != EndpointGatewayBot {
			t.Errorf("request = %s %s", request.Method, request.URL)
		}
		if got := request.Header.Get("Authorization"); got != "Bot rest-token" {
			t.Errorf("Authorization = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"url":"wss://gateway.example.test",
				"shards":3,
				"session_start_limit":{
					"total":1000,
					"remaining":9,
					"reset_after":60000,
					"max_concurrency":2
				}
			}`)),
			Request: request,
		}, nil
	})

	var factoryCalls []int
	manager, err := NewShardManagerWithConfig(context.Background(), ShardManagerConfig{
		RESTSession: restSession,
		NewSession: func(shardID, shardCount int) (*Session, error) {
			if shardCount != 3 {
				t.Errorf("factory shardCount = %d, want 3", shardCount)
			}
			factoryCalls = append(factoryCalls, shardID)
			return New("Bot shard-token")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if restCalls.Load() != 1 {
		t.Fatalf("Get Gateway Bot calls = %d, want 1", restCalls.Load())
	}
	if got := manager.GatewayBot(); got.URL != "wss://gateway.example.test/" || got.Shards != 3 {
		t.Fatalf("GatewayBot = %#v", got)
	}
	if got := manager.SessionStartLimit(); got.Total != 1000 ||
		got.Remaining != 9 || got.MaxConcurrency != 2 {
		t.Fatalf("SessionStartLimit = %#v", got)
	}
	if len(factoryCalls) != 3 {
		t.Fatalf("factory calls = %v", factoryCalls)
	}

	sessions := manager.Sessions()
	if len(sessions) != 3 {
		t.Fatalf("Sessions length = %d, want 3", len(sessions))
	}
	for shardID, session := range sessions {
		session.RLock()
		gotShardID := session.ShardID
		gotShardCount := session.ShardCount
		gotIdentifyShard := session.Identify.Shard
		gotCoordinator := session.IdentifyCoordinator
		gotGateway := session.gateway
		session.RUnlock()
		if gotShardID != shardID || gotShardCount != 3 {
			t.Errorf(
				"session %d shard = (%d, %d), want (%d, 3)",
				shardID,
				gotShardID,
				gotShardCount,
				shardID,
			)
		}
		if gotIdentifyShard == nil || *gotIdentifyShard != [2]int{shardID, 3} {
			t.Errorf("session %d Identify.Shard = %v", shardID, gotIdentifyShard)
		}
		if gotCoordinator != manager.Coordinator() {
			t.Errorf("session %d does not share the manager coordinator", shardID)
		}
		if gotGateway != "wss://gateway.example.test/" {
			t.Errorf("session %d gateway = %q", shardID, gotGateway)
		}
	}

	copied := manager.Sessions()
	copied[0] = nil
	if session, ok := manager.Session(0); !ok || session == nil {
		t.Fatal("Sessions returned the manager's mutable backing slice")
	}
	if _, ok := manager.Session(-1); ok {
		t.Fatal("Session accepted a negative shard ID")
	}
	if _, ok := manager.Session(3); ok {
		t.Fatal("Session accepted an out-of-range shard ID")
	}
}

func TestShardManagerOpenAndClose(t *testing.T) {
	t.Run("opens all shards concurrently and closes once", func(t *testing.T) {
		manager := newTestShardManager(t, 3)
		var arrivals atomic.Int32
		release := make(chan struct{})
		manager.openSession = func(context.Context, *Session) error {
			if arrivals.Add(1) == 3 {
				close(release)
			}
			select {
			case <-release:
				return nil
			case <-time.After(time.Second):
				return errors.New("shards did not open concurrently")
			}
		}

		closeCounts := make([]atomic.Int32, 3)
		manager.closeSession = func(session *Session) error {
			closeCounts[session.ShardID].Add(1)
			return nil
		}

		if err := manager.Open(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := manager.Open(context.Background()); !errors.Is(err, ErrShardManagerAlreadyOpen) {
			t.Fatalf("second Open error = %v", err)
		}
		if err := manager.Close(); err != nil {
			t.Fatal(err)
		}
		if err := manager.Close(); err != nil {
			t.Fatal(err)
		}
		for shardID := range closeCounts {
			if got := closeCounts[shardID].Load(); got != 1 {
				t.Errorf("shard %d close count = %d, want 1", shardID, got)
			}
		}
	})

	t.Run("partial failure cancels and closes every shard", func(t *testing.T) {
		manager := newTestShardManager(t, 3)
		openFailure := errors.New("fake Gateway rejected shard")
		var arrivals atomic.Int32
		allStarted := make(chan struct{})
		manager.openSession = func(ctx context.Context, session *Session) error {
			if arrivals.Add(1) == 3 {
				close(allStarted)
			}
			select {
			case <-allStarted:
			case <-ctx.Done():
				return ctx.Err()
			}
			if session.ShardID == 1 {
				return openFailure
			}
			<-ctx.Done()
			return ctx.Err()
		}

		closeCounts := make([]atomic.Int32, 3)
		manager.closeSession = func(session *Session) error {
			closeCounts[session.ShardID].Add(1)
			return nil
		}

		err := manager.Open(context.Background())
		if !errors.Is(err, openFailure) {
			t.Fatalf("Open error = %v, want fake Gateway failure", err)
		}
		for shardID := range closeCounts {
			if got := closeCounts[shardID].Load(); got != 1 {
				t.Errorf("shard %d close count = %d, want 1", shardID, got)
			}
		}
		if err = manager.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestShardManagerOpensFakeGatewayWithIdentifyBuckets(t *testing.T) {
	identified := make(chan int, 3)
	gatewayErrors := make(chan error, 3)
	server := newGatewayTestServer(t, func(server *gatewayTestServer, _ int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, time.Second); err != nil {
			gatewayErrors <- err
			return
		}
		for {
			operation, data, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			switch operation {
			case 1:
				if err = ws.WriteJSON(map[string]interface{}{"op": 11}); err != nil {
					gatewayErrors <- err
					return
				}
			case 2:
				var identify struct {
					Shard *[2]int `json:"shard"`
				}
				if err = json.Unmarshal(data, &identify); err != nil {
					gatewayErrors <- err
					return
				}
				if identify.Shard == nil {
					gatewayErrors <- errors.New("Identify omitted its shard")
					return
				}
				shardID := identify.Shard[0]
				identified <- shardID
				if err = writeGatewayReady(
					ws,
					server.url,
					fmt.Sprintf("manager-shard-%d", shardID),
				); err != nil {
					gatewayErrors <- err
					return
				}
			}
		}
	})

	restSession, err := New("Bot manager-gateway")
	if err != nil {
		t.Fatal(err)
	}
	restSession.Client.Transport = shardingRoundTripper(func(request *http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{
			"url":%q,
			"shards":3,
			"session_start_limit":{
				"total":10,
				"remaining":10,
				"reset_after":60000,
				"max_concurrency":2
			}
		}`, server.url)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})
	manager, err := NewShardManagerWithConfig(context.Background(), ShardManagerConfig{
		Token:       "Bot manager-gateway",
		RESTSession: restSession,
	})
	if err != nil {
		t.Fatal(err)
	}

	clock := &advancingIdentifyClock{now: time.Unix(600, 0)}
	coordinator, err := newIdentifyCoordinatorWithClock(
		manager.GatewayBot().SessionStartLimit,
		clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	manager.coordinator = coordinator
	manager.mu.Unlock()
	for _, session := range manager.Sessions() {
		session.Lock()
		session.IdentifyCoordinator = coordinator
		session.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err = manager.Open(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()

	gotShards := make(map[int]bool, 3)
	for range 3 {
		select {
		case shardID := <-identified:
			gotShards[shardID] = true
		case gatewayErr := <-gatewayErrors:
			t.Fatal(gatewayErr)
		case <-ctx.Done():
			t.Fatal("timed out waiting for managed shard Identifies")
		}
	}
	for shardID := range 3 {
		if !gotShards[shardID] {
			t.Errorf("shard %d did not Identify", shardID)
		}
	}
	if waits := clock.Waits(); len(waits) != 1 || waits[0] != identifyConcurrencyWindow {
		t.Fatalf("Identify bucket waits = %v, want [%s]", waits, identifyConcurrencyWindow)
	}
	if got := coordinator.Status().Remaining; got != 7 {
		t.Fatalf("remaining = %d, want 7", got)
	}
}

func TestGatewayResumeDoesNotConsumeIdentifyQuota(t *testing.T) {
	operations := make(chan int, 1)
	server := newGatewayTestServer(t, func(_ *gatewayTestServer, _ int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, time.Second); err != nil {
			return
		}
		for {
			operation, _, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			switch operation {
			case 1:
				if err = ws.WriteJSON(map[string]interface{}{"op": 11}); err != nil {
					return
				}
			case 6:
				operations <- operation
				if err = ws.WriteJSON(map[string]interface{}{
					"op": 0,
					"s":  2,
					"t":  "RESUMED",
					"d":  map[string]interface{}{},
				}); err != nil {
					return
				}
			}
		}
	})

	session, err := New("Bot resume-test")
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewIdentifyCoordinator(SessionInformation{
		Total:          10,
		Remaining:      4,
		ResetAfter:     60_000,
		MaxConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	session.IdentifyCoordinator = coordinator
	session.gatewaySessionMu.Lock()
	session.sessionID = "resumable-session"
	session.resumeGatewayURL = server.url
	session.gatewaySessionMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err = session.OpenWithContext(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case operation := <-operations:
		if operation != 6 {
			t.Fatalf("Gateway operation = %d, want Resume (6)", operation)
		}
	default:
		t.Fatal("Gateway did not receive Resume")
	}
	if got := coordinator.Status().Remaining; got != 4 {
		t.Fatalf("remaining after Resume = %d, want 4", got)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
}

func newTestShardManager(t *testing.T, shardCount int) *ShardManager {
	t.Helper()
	restSession, err := New("Bot manager-test")
	if err != nil {
		t.Fatal(err)
	}
	restSession.Client.Transport = shardingRoundTripper(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"url":"wss://gateway.example.test/",
				"shards":3,
				"session_start_limit":{
					"total":1000,
					"remaining":1000,
					"reset_after":60000,
					"max_concurrency":2
				}
			}`)),
			Request: request,
		}, nil
	})

	manager, err := NewShardManagerWithConfig(context.Background(), ShardManagerConfig{
		RESTSession: restSession,
		NewSession: func(_, _ int) (*Session, error) {
			return New("Bot manager-test")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if shardCount != len(manager.sessions) {
		t.Fatalf("test helper supports %d shards, requested %d", len(manager.sessions), shardCount)
	}
	return manager
}
