package dgo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const identifyConcurrencyWindow = 5 * time.Second

var (
	// ErrInvalidSessionStartLimit indicates that Discord returned unusable
	// session-start-limit values.
	ErrInvalidSessionStartLimit = errors.New("invalid gateway session start limit")

	// ErrIdentifyQuotaExhausted indicates that no fresh Gateway sessions may be
	// started until the session-start-limit reset.
	ErrIdentifyQuotaExhausted = errors.New("gateway identify quota exhausted")

	// ErrInvalidShardID indicates that an Identify was requested for a negative
	// shard ID.
	ErrInvalidShardID = errors.New("shard ID must not be negative")

	// ErrShardManagerAlreadyOpen indicates that Open was called while the
	// manager was opening, open, or closing.
	ErrShardManagerAlreadyOpen = errors.New("shard manager already open")
)

// IdentifyQuotaExhaustedError describes an exhausted Gateway session-start
// quota and the time remaining until Discord resets it.
type IdentifyQuotaExhaustedError struct {
	Total      int
	RetryAfter time.Duration
}

func (e *IdentifyQuotaExhaustedError) Error() string {
	return fmt.Sprintf(
		"%v: total=%d retry_after=%s",
		ErrIdentifyQuotaExhausted,
		e.Total,
		e.RetryAfter,
	)
}

// Unwrap supports errors.Is(err, ErrIdentifyQuotaExhausted).
func (e *IdentifyQuotaExhaustedError) Unwrap() error {
	return ErrIdentifyQuotaExhausted
}

// IdentifyCoordinatorStatus is a concurrency-safe snapshot of Discord's
// Gateway session-start limit.
type IdentifyCoordinatorStatus struct {
	Total          int
	Remaining      int
	ResetAfter     time.Duration
	ResetAt        time.Time
	MaxConcurrency int
}

type identifyClock interface {
	Now() time.Time
	Wait(context.Context, time.Duration) error
}

type realIdentifyClock struct{}

func (realIdentifyClock) Now() time.Time {
	return time.Now()
}

func (realIdentifyClock) Wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IdentifyCoordinator manages Discord's global Identify quota and the
// five-second gate for each max_concurrency bucket.
//
// Acquire must be called only for a fresh Identify. Resume does not consume
// the session-start quota and must bypass this coordinator.
type IdentifyCoordinator struct {
	mu sync.Mutex

	total          int
	remaining      int
	resetWindow    time.Duration
	resetAt        time.Time
	maxConcurrency int
	bucketReadyAt  map[int]time.Time
	clock          identifyClock
}

// NewIdentifyCoordinator creates a coordinator from Get Gateway Bot's
// session_start_limit response.
func NewIdentifyCoordinator(limit SessionInformation) (*IdentifyCoordinator, error) {
	return newIdentifyCoordinatorWithClock(limit, realIdentifyClock{})
}

func newIdentifyCoordinatorWithClock(
	limit SessionInformation,
	clock identifyClock,
) (*IdentifyCoordinator, error) {
	if clock == nil {
		return nil, fmt.Errorf("%w: clock is nil", ErrInvalidSessionStartLimit)
	}
	if err := validateSessionStartLimit(limit); err != nil {
		return nil, err
	}

	now := clock.Now()
	return &IdentifyCoordinator{
		total:          limit.Total,
		remaining:      limit.Remaining,
		resetWindow:    time.Duration(limit.ResetAfter) * time.Millisecond,
		resetAt:        now.Add(time.Duration(limit.ResetAfter) * time.Millisecond),
		maxConcurrency: limit.MaxConcurrency,
		bucketReadyAt:  make(map[int]time.Time, limit.MaxConcurrency),
		clock:          clock,
	}, nil
}

func validateSessionStartLimit(limit SessionInformation) error {
	switch {
	case limit.Total <= 0:
		return fmt.Errorf("%w: total must be positive", ErrInvalidSessionStartLimit)
	case limit.Remaining < 0 || limit.Remaining > limit.Total:
		return fmt.Errorf(
			"%w: remaining must be between zero and total",
			ErrInvalidSessionStartLimit,
		)
	case limit.ResetAfter <= 0:
		return fmt.Errorf("%w: reset_after must be positive", ErrInvalidSessionStartLimit)
	case limit.MaxConcurrency <= 0:
		return fmt.Errorf(
			"%w: max_concurrency must be positive",
			ErrInvalidSessionStartLimit,
		)
	default:
		return nil
	}
}

// Acquire waits until shardID's concurrency bucket may send a fresh Identify,
// then atomically consumes one session-start from the remaining quota.
func (c *IdentifyCoordinator) Acquire(ctx context.Context, shardID int) error {
	if ctx == nil {
		return errors.New("identify context must not be nil")
	}
	if shardID < 0 {
		return ErrInvalidShardID
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		c.mu.Lock()
		now := c.clock.Now()
		c.resetQuotaLocked(now)
		if c.remaining == 0 {
			retryAfter := c.resetAt.Sub(now)
			if retryAfter < 0 {
				retryAfter = 0
			}
			err := &IdentifyQuotaExhaustedError{
				Total:      c.total,
				RetryAfter: retryAfter,
			}
			c.mu.Unlock()
			return err
		}

		bucket := shardID % c.maxConcurrency
		readyAt := c.bucketReadyAt[bucket]
		if now.Before(readyAt) {
			delay := readyAt.Sub(now)
			c.mu.Unlock()
			if err := c.clock.Wait(ctx, delay); err != nil {
				return err
			}
			continue
		}

		c.remaining--
		c.bucketReadyAt[bucket] = now.Add(identifyConcurrencyWindow)
		c.mu.Unlock()
		return nil
	}
}

// Refresh replaces the quota information with a newer Get Gateway Bot
// response. Existing bucket gates are retained unless max_concurrency changes.
func (c *IdentifyCoordinator) Refresh(limit SessionInformation) error {
	if err := validateSessionStartLimit(limit); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxConcurrency != limit.MaxConcurrency {
		c.bucketReadyAt = make(map[int]time.Time, limit.MaxConcurrency)
	}
	c.total = limit.Total
	c.remaining = limit.Remaining
	c.resetWindow = time.Duration(limit.ResetAfter) * time.Millisecond
	c.resetAt = c.clock.Now().Add(c.resetWindow)
	c.maxConcurrency = limit.MaxConcurrency
	return nil
}

// Status returns the current session-start-limit snapshot. ResetAfter is the
// duration remaining until ResetAt, not the original REST response value.
func (c *IdentifyCoordinator) Status() IdentifyCoordinatorStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clock.Now()
	c.resetQuotaLocked(now)
	resetAfter := c.resetAt.Sub(now)
	if resetAfter < 0 {
		resetAfter = 0
	}
	return IdentifyCoordinatorStatus{
		Total:          c.total,
		Remaining:      c.remaining,
		ResetAfter:     resetAfter,
		ResetAt:        c.resetAt,
		MaxConcurrency: c.maxConcurrency,
	}
}

// SessionStartLimit returns the current quota in the same shape and
// millisecond unit used by Get Gateway Bot.
func (c *IdentifyCoordinator) SessionStartLimit() SessionInformation {
	status := c.Status()
	return SessionInformation{
		Total:          status.Total,
		Remaining:      status.Remaining,
		ResetAfter:     durationMillisecondsCeil(status.ResetAfter),
		MaxConcurrency: status.MaxConcurrency,
	}
}

func (c *IdentifyCoordinator) resetQuotaLocked(now time.Time) {
	if now.Before(c.resetAt) {
		return
	}
	c.remaining = c.total
	c.resetAt = now.Add(c.resetWindow)
}

func durationMillisecondsCeil(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	milliseconds := duration / time.Millisecond
	if duration%time.Millisecond != 0 {
		milliseconds++
	}
	return int(milliseconds)
}

// ShardSessionFactory creates one Session for a recommended shard.
type ShardSessionFactory func(shardID, shardCount int) (*Session, error)

// ShardManagerConfig configures Gateway discovery and shard Session creation.
type ShardManagerConfig struct {
	// Token is used by the default REST Session and shard factory.
	Token string

	// RESTSession may provide a custom HTTP client or an already configured
	// authentication token for Get Gateway Bot.
	RESTSession *Session

	// NewSession overrides creation of each shard Session.
	NewSession ShardSessionFactory

	// RequestOptions are applied to Get Gateway Bot.
	RequestOptions []RequestOption
}

// ShardOpenError identifies a shard that failed to open or close.
type ShardOpenError struct {
	ShardID   int
	Operation string
	Err       error
}

func (e *ShardOpenError) Error() string {
	return fmt.Sprintf("shard %d %s failed: %v", e.ShardID, e.Operation, e.Err)
}

// Unwrap exposes the underlying Session error.
func (e *ShardOpenError) Unwrap() error {
	return e.Err
}

type shardManagerState uint8

const (
	shardManagerClosed shardManagerState = iota
	shardManagerOpening
	shardManagerOpen
	shardManagerClosing
)

// ShardManager owns the recommended Gateway shard Sessions and coordinates
// their fresh Identify requests.
type ShardManager struct {
	mu sync.Mutex

	gateway     GatewayBotResponse
	coordinator *IdentifyCoordinator
	sessions    []*Session

	state            shardManagerState
	cancel           context.CancelFunc
	openDone         chan struct{}
	closeDone        chan struct{}
	cleanupCompleted bool

	openSession  func(context.Context, *Session) error
	closeSession func(*Session) error
}

// NewShardManager discovers Discord's recommended shard count using token.
func NewShardManager(
	ctx context.Context,
	token string,
	options ...RequestOption,
) (*ShardManager, error) {
	return NewShardManagerWithConfig(ctx, ShardManagerConfig{
		Token:          token,
		RequestOptions: options,
	})
}

// NewShardManagerWithConfig discovers Discord's recommended shard count,
// creates every Session, and assigns its shard ID and count. It does not open
// the Gateway connections; call Open for that.
func NewShardManagerWithConfig(
	ctx context.Context,
	config ShardManagerConfig,
) (*ShardManager, error) {
	if ctx == nil {
		return nil, errors.New("shard manager context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	restSession := config.RESTSession
	var err error
	if restSession == nil {
		restSession, err = New(config.Token)
		if err != nil {
			return nil, fmt.Errorf("create Gateway REST session: %w", err)
		}
	}

	requestOptions := append([]RequestOption(nil), config.RequestOptions...)
	requestOptions = append(requestOptions, WithContext(ctx))
	gateway, err := restSession.GatewayBot(requestOptions...)
	if err != nil {
		return nil, fmt.Errorf("get Gateway Bot: %w", err)
	}
	if gateway == nil || gateway.URL == "" {
		return nil, errors.New("get Gateway Bot returned an empty URL")
	}
	if gateway.Shards <= 0 {
		return nil, fmt.Errorf(
			"get Gateway Bot returned invalid recommended shard count %d",
			gateway.Shards,
		)
	}

	coordinator, err := NewIdentifyCoordinator(gateway.SessionStartLimit)
	if err != nil {
		return nil, fmt.Errorf("get Gateway Bot: %w", err)
	}

	token := config.Token
	if token == "" {
		token = restSession.gatewayToken()
	}
	factory := config.NewSession
	if factory == nil {
		factory = func(_, _ int) (*Session, error) {
			return New(token)
		}
	}

	manager := &ShardManager{
		gateway:     *gateway,
		coordinator: coordinator,
		sessions:    make([]*Session, 0, gateway.Shards),
		state:       shardManagerClosed,
		openSession: func(ctx context.Context, session *Session) error {
			return session.OpenWithContext(ctx)
		},
		closeSession: func(session *Session) error {
			return session.Close()
		},
	}

	for shardID := range gateway.Shards {
		session, factoryErr := factory(shardID, gateway.Shards)
		if factoryErr != nil {
			cleanupErr := manager.closeSessions(manager.sessions)
			return nil, errors.Join(
				fmt.Errorf("create shard %d: %w", shardID, factoryErr),
				cleanupErr,
			)
		}
		if session == nil {
			cleanupErr := manager.closeSessions(manager.sessions)
			return nil, errors.Join(
				fmt.Errorf("create shard %d: factory returned nil Session", shardID),
				cleanupErr,
			)
		}

		session.Lock()
		session.ShardID = shardID
		session.ShardCount = gateway.Shards
		session.Identify.Shard = &[2]int{shardID, gateway.Shards}
		session.IdentifyCoordinator = coordinator
		session.gateway = gateway.URL
		session.Unlock()
		manager.sessions = append(manager.sessions, session)
	}

	return manager, nil
}

// GatewayBot returns the immutable Gateway discovery response used to create
// this manager.
func (m *ShardManager) GatewayBot() GatewayBotResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gateway
}

// Coordinator returns the Identify coordinator shared by every shard.
func (m *ShardManager) Coordinator() *IdentifyCoordinator {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.coordinator
}

// SessionStartLimit returns the coordinator's current quota snapshot.
func (m *ShardManager) SessionStartLimit() SessionInformation {
	return m.Coordinator().SessionStartLimit()
}

// Sessions returns a copy of the manager's shard Session slice.
func (m *ShardManager) Sessions() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*Session(nil), m.sessions...)
}

// Session returns one shard Session by ID.
func (m *ShardManager) Session(shardID int) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if shardID < 0 || shardID >= len(m.sessions) {
		return nil, false
	}
	return m.sessions[shardID], true
}

// Open starts every shard concurrently. If any shard fails, Open cancels the
// shared lifetime and closes every shard, including those already connected.
func (m *ShardManager) Open(ctx context.Context) error {
	if ctx == nil {
		return errors.New("shard manager context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	if m.state != shardManagerClosed {
		m.mu.Unlock()
		return ErrShardManagerAlreadyOpen
	}
	lifetime, cancel := context.WithCancel(ctx)
	openDone := make(chan struct{})
	m.state = shardManagerOpening
	m.cancel = cancel
	m.openDone = openDone
	m.cleanupCompleted = false
	sessions := append([]*Session(nil), m.sessions...)
	m.mu.Unlock()

	openErrors := make([]error, len(sessions))
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(sessions))
	for shardID, session := range sessions {
		go func() {
			defer waitGroup.Done()
			if err := m.openSession(lifetime, session); err != nil {
				openErrors[shardID] = &ShardOpenError{
					ShardID:   shardID,
					Operation: "open",
					Err:       err,
				}
				cancel()
			}
		}()
	}
	waitGroup.Wait()

	var resultErrors []error
	for _, openErr := range openErrors {
		if openErr != nil {
			resultErrors = append(resultErrors, openErr)
		}
	}
	if len(resultErrors) == 0 {
		if err := lifetime.Err(); err != nil {
			resultErrors = append(resultErrors, err)
		}
	}

	m.mu.Lock()
	closing := m.state == shardManagerClosing
	if len(resultErrors) == 0 && !closing {
		m.state = shardManagerOpen
		m.openDone = nil
		close(openDone)
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	if cleanupErr := m.closeSessions(sessions); cleanupErr != nil {
		resultErrors = append(resultErrors, cleanupErr)
	}

	m.mu.Lock()
	m.cleanupCompleted = true
	m.cancel = nil
	m.openDone = nil
	if m.state == shardManagerOpening {
		m.state = shardManagerClosed
	}
	close(openDone)
	m.mu.Unlock()

	if len(resultErrors) == 0 {
		return context.Canceled
	}
	return errors.Join(resultErrors...)
}

// Close cancels the shared Gateway lifetime and closes every shard. It is
// safe to call more than once.
func (m *ShardManager) Close() error {
	m.mu.Lock()
	switch m.state {
	case shardManagerClosed:
		m.mu.Unlock()
		return nil
	case shardManagerClosing:
		closeDone := m.closeDone
		m.mu.Unlock()
		<-closeDone
		return nil
	}

	m.state = shardManagerClosing
	closeDone := make(chan struct{})
	m.closeDone = closeDone
	cancel := m.cancel
	openDone := m.openDone
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if openDone != nil {
		<-openDone
	}

	m.mu.Lock()
	cleanupCompleted := m.cleanupCompleted
	sessions := append([]*Session(nil), m.sessions...)
	m.mu.Unlock()

	var err error
	if !cleanupCompleted {
		err = m.closeSessions(sessions)
	}

	m.mu.Lock()
	m.state = shardManagerClosed
	m.cancel = nil
	m.openDone = nil
	m.cleanupCompleted = true
	m.closeDone = nil
	close(closeDone)
	m.mu.Unlock()
	return err
}

func (m *ShardManager) closeSessions(sessions []*Session) error {
	closeErrors := make([]error, len(sessions))
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(sessions))
	for shardID, session := range sessions {
		go func() {
			defer waitGroup.Done()
			if err := m.closeSession(session); err != nil {
				closeErrors[shardID] = &ShardOpenError{
					ShardID:   shardID,
					Operation: "close",
					Err:       err,
				}
			}
		}()
	}
	waitGroup.Wait()

	var result []error
	for _, closeErr := range closeErrors {
		if closeErr != nil {
			result = append(result, closeErr)
		}
	}
	return errors.Join(result...)
}
