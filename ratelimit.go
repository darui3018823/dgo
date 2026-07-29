package dgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultGlobalRateLimit             = 50
	defaultGlobalRateWindow            = time.Second
	defaultRateLimitBucketTTL          = 10 * time.Minute
	defaultMaxRateLimitBuckets         = 10_000
	defaultMaxRateLimitBucketMappings  = 20_000
	defaultInvalidRequestLimit         = 10_000
	defaultInvalidRequestWarning       = 8_000
	defaultInvalidRequestWindow        = 10 * time.Minute
	rateLimitBucketCleanupInterval     = time.Minute
	rateLimitRouteKeyPrefix            = "route:"
	rateLimitDiscoveredBucketKeyPrefix = "bucket:"
	rateLimitMajorKeyPrefix            = "major:"
)

var (
	// ErrInvalidRequestLimit is returned when Discord's invalid-request
	// threshold has been reached. Requests are blocked until the rolling
	// invalid-request window has room again.
	ErrInvalidRequestLimit = errors.New("discord invalid-request limit reached")
)

// RateLimitScope describes how Discord applies a REST rate-limit bucket.
type RateLimitScope string

const (
	RateLimitScopeUser   RateLimitScope = "user"
	RateLimitScopeGlobal RateLimitScope = "global"
	RateLimitScopeShared RateLimitScope = "shared"
)

// InvalidRequestStatus describes the current 401/403/429 rolling-window budget.
type InvalidRequestStatus struct {
	Count            int
	Limit            int
	WarningThreshold int
	Window           time.Duration
	ResetAfter       time.Duration
	Warning          bool
	WarningTriggered bool
	Blocked          bool
}

// InvalidRequestLimitError reports that the invalid-request circuit breaker is
// active.
type InvalidRequestLimitError struct {
	Count      int
	Limit      int
	ResetAfter time.Duration
}

func (e InvalidRequestLimitError) Error() string {
	return fmt.Sprintf(
		"%s (%d/%d; retry after %s)",
		ErrInvalidRequestLimit,
		e.Count,
		e.Limit,
		e.ResetAfter,
	)
}

// Unwrap supports errors.Is(err, ErrInvalidRequestLimit).
func (e InvalidRequestLimitError) Unwrap() error {
	return ErrInvalidRequestLimit
}

// RateLimiter holds all rate-limit buckets.
type RateLimiter struct {
	sync.Mutex
	global         *int64
	buckets        map[string]*Bucket
	bucketHashes   map[string]string // maps normalized route+major keys to discovered bucket keys
	bucketHashUsed map[string]int64

	// GlobalRateLimit and GlobalRateWindow proactively enforce Discord's
	// default bot-wide request budget. Set GlobalRateLimit to zero to disable
	// proactive limiting; server-provided global limits are always honored.
	GlobalRateLimit  int
	GlobalRateWindow time.Duration

	// BucketTTL, MaxBuckets, and MaxBucketMappings bound the in-memory
	// route/bucket cache.
	BucketTTL         time.Duration
	MaxBuckets        int
	MaxBucketMappings int

	// InvalidRequestLimit tracks 401, 403, and 429 responses in a rolling
	// window. Set it to zero to disable the circuit breaker.
	InvalidRequestLimit            int
	InvalidRequestWarningThreshold int
	InvalidRequestWindow           time.Duration

	globalWindowStart time.Time
	globalRequests    int
	lastCleanup       time.Time

	invalidRequests []time.Time
	invalidWarned   bool
}

// NewRatelimiter returns a new RateLimiter.
func NewRatelimiter() *RateLimiter {
	return &RateLimiter{
		buckets:                        make(map[string]*Bucket),
		bucketHashes:                   make(map[string]string),
		bucketHashUsed:                 make(map[string]int64),
		global:                         new(int64),
		GlobalRateLimit:                defaultGlobalRateLimit,
		GlobalRateWindow:               defaultGlobalRateWindow,
		BucketTTL:                      defaultRateLimitBucketTTL,
		MaxBuckets:                     defaultMaxRateLimitBuckets,
		MaxBucketMappings:              defaultMaxRateLimitBucketMappings,
		InvalidRequestLimit:            defaultInvalidRequestLimit,
		InvalidRequestWarningThreshold: defaultInvalidRequestWarning,
		InvalidRequestWindow:           defaultInvalidRequestWindow,
	}
}

// GetBucket retrieves or creates a legacy bucket by caller-supplied key.
func (r *RateLimiter) GetBucket(key string) *Bucket {
	return r.getBucket(key, "", "")
}

func (r *RateLimiter) getRouteBucket(routeKey, majorKey string) *Bucket {
	key := rateLimitRouteBucketKey(routeKey, majorKey)
	return r.getBucket(key, routeKey, majorKey)
}

func (r *RateLimiter) getBucket(key, routeKey, majorKey string) *Bucket {
	now := time.Now()
	r.Lock()
	defer r.Unlock()

	r.cleanupBucketsLocked(now)

	if target, ok := r.bucketHashes[key]; ok {
		if bucket, ok := r.buckets[target]; ok {
			r.bucketHashUsed[key] = now.UnixNano()
			atomic.StoreInt64(&bucket.lastUsed, now.UnixNano())
			return bucket
		}
		delete(r.bucketHashes, key)
		delete(r.bucketHashUsed, key)
	}

	if bucket, ok := r.buckets[key]; ok {
		atomic.StoreInt64(&bucket.lastUsed, now.UnixNano())
		return bucket
	}

	r.makeBucketRoomLocked(1)
	b := &Bucket{
		Remaining:   1,
		Key:         key,
		RouteKey:    routeKey,
		MajorKey:    majorKey,
		global:      r.global,
		ratelimiter: r,
		lastUsed:    now.UnixNano(),
	}

	r.buckets[key] = b
	return b
}

func (r *RateLimiter) cleanupBucketsLocked(now time.Time) {
	if r.BucketTTL <= 0 {
		return
	}
	interval := rateLimitBucketCleanupInterval
	if halfTTL := r.BucketTTL / 2; halfTTL > 0 && halfTTL < interval {
		interval = halfTTL
	}
	if !r.lastCleanup.IsZero() && now.Sub(r.lastCleanup) < interval {
		return
	}
	r.lastCleanup = now

	cutoff := now.Add(-r.BucketTTL).UnixNano()
	for key, bucket := range r.buckets {
		if atomic.LoadInt64(&bucket.lastUsed) < cutoff {
			delete(r.buckets, key)
		}
	}
	for routeKey, used := range r.bucketHashUsed {
		if used < cutoff {
			delete(r.bucketHashes, routeKey)
			delete(r.bucketHashUsed, routeKey)
		}
	}
	r.removeDanglingBucketHashesLocked()
}

func (r *RateLimiter) makeBucketRoomLocked(extra int) {
	if r.MaxBuckets <= 0 {
		return
	}
	for len(r.buckets)+extra > r.MaxBuckets {
		var oldestKey string
		var oldestUsed int64
		for key, bucket := range r.buckets {
			used := atomic.LoadInt64(&bucket.lastUsed)
			if oldestKey == "" || used < oldestUsed {
				oldestKey = key
				oldestUsed = used
			}
		}
		if oldestKey == "" {
			break
		}
		delete(r.buckets, oldestKey)
	}
	r.removeDanglingBucketHashesLocked()
}

func (r *RateLimiter) removeDanglingBucketHashesLocked() {
	for routeKey, bucketKey := range r.bucketHashes {
		if _, ok := r.buckets[bucketKey]; !ok {
			delete(r.bucketHashes, routeKey)
			delete(r.bucketHashUsed, routeKey)
		}
	}
}

func (r *RateLimiter) setBucketHashLocked(routeKey, bucketKey string, now time.Time) {
	if _, exists := r.bucketHashes[routeKey]; !exists && r.MaxBucketMappings > 0 {
		for len(r.bucketHashes) >= r.MaxBucketMappings {
			var oldestKey string
			var oldestUsed int64
			for key, used := range r.bucketHashUsed {
				if oldestKey == "" || used < oldestUsed {
					oldestKey = key
					oldestUsed = used
				}
			}
			if oldestKey == "" {
				break
			}
			delete(r.bucketHashes, oldestKey)
			delete(r.bucketHashUsed, oldestKey)
		}
	}
	r.bucketHashes[routeKey] = bucketKey
	r.bucketHashUsed[routeKey] = now.UnixNano()
}

// GetWaitTime returns the duration to wait before using a Bucket.
func (r *RateLimiter) GetWaitTime(b *Bucket, minRemaining int) time.Duration {
	now := time.Now()
	var wait time.Duration

	if b.Remaining < minRemaining && b.reset.After(now) {
		wait = b.reset.Sub(now)
	}

	sleepTo := time.Unix(0, atomic.LoadInt64(r.global))
	if globalWait := sleepTo.Sub(now); globalWait > wait {
		wait = globalWait
	}

	if proactiveWait := r.proactiveGlobalWait(now); proactiveWait > wait {
		wait = proactiveWait
	}
	return wait
}

func (r *RateLimiter) proactiveGlobalWait(now time.Time) time.Duration {
	r.Lock()
	defer r.Unlock()
	r.resetGlobalWindowLocked(now)
	if r.GlobalRateLimit <= 0 || r.globalRequests < r.GlobalRateLimit {
		return 0
	}
	wait := r.globalWindowStart.Add(r.globalRateWindowLocked()).Sub(now)
	if wait < 0 {
		return 0
	}
	return wait
}

func (r *RateLimiter) reserveGlobalRequest(now time.Time) (time.Duration, bool) {
	r.Lock()
	defer r.Unlock()
	r.resetGlobalWindowLocked(now)
	if r.GlobalRateLimit <= 0 {
		return 0, true
	}
	if r.globalRequests >= r.GlobalRateLimit {
		wait := r.globalWindowStart.Add(r.globalRateWindowLocked()).Sub(now)
		if wait < 0 {
			wait = 0
		}
		return wait, false
	}
	r.globalRequests++
	return 0, true
}

func (r *RateLimiter) resetGlobalWindowLocked(now time.Time) {
	window := r.globalRateWindowLocked()
	if r.globalWindowStart.IsZero() || now.Sub(r.globalWindowStart) >= window {
		r.globalWindowStart = now
		r.globalRequests = 0
	}
}

func (r *RateLimiter) globalRateWindowLocked() time.Duration {
	if r.GlobalRateWindow <= 0 {
		return defaultGlobalRateWindow
	}
	return r.GlobalRateWindow
}

// LockBucket locks until a request can be made.
func (r *RateLimiter) LockBucket(bucketID string) *Bucket {
	b, _ := r.LockBucketContext(context.Background(), bucketID)
	return b
}

// LockBucketContext locks until a request can be made or context is cancelled.
func (r *RateLimiter) LockBucketContext(ctx context.Context, bucketID string) (*Bucket, error) {
	return r.LockBucketObjectContext(ctx, r.GetBucket(bucketID))
}

// LockBucketRouteContext resolves a normalized REST route and its hashed major
// parameter, then locks until a request can be made.
func (r *RateLimiter) LockBucketRouteContext(ctx context.Context, routeKey, majorKey string) (*Bucket, error) {
	if err := r.CheckInvalidRequestLimit(); err != nil {
		return nil, err
	}
	return r.LockBucketObjectContext(ctx, r.getRouteBucket(routeKey, majorKey))
}

// LockBucketObject locks an already resolved bucket until a request can be made.
func (r *RateLimiter) LockBucketObject(b *Bucket) *Bucket {
	b, _ = r.LockBucketObjectContext(context.Background(), b)
	return b
}

// LockBucketObjectContext locks an already resolved bucket until a request can
// be made or context is cancelled.
func (r *RateLimiter) LockBucketObjectContext(ctx context.Context, b *Bucket) (*Bucket, error) {
	if b == nil {
		return nil, fmt.Errorf("rate limit bucket is nil")
	}
	b.Lock()

	for {
		wait := r.GetWaitTime(b, 1)
		if wait <= 0 {
			var reserved bool
			wait, reserved = r.reserveGlobalRequest(time.Now())
			if reserved {
				break
			}
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			b.Unlock()
			return nil, ctx.Err()
		case <-timer.C:
			// Re-evaluate route and global limits because another request may
			// have extended either deadline while this bucket was waiting.
		}
	}

	b.Remaining--
	atomic.StoreInt64(&b.lastUsed, time.Now().UnixNano())
	return b, nil
}

// Bucket represents one Discord rate-limit bucket.
type Bucket struct {
	sync.Mutex
	Key       string
	RouteKey  string
	MajorKey  string
	Scope     RateLimitScope
	Limit     int
	Remaining int
	reset     time.Time
	global    *int64

	ratelimiter *RateLimiter
	lastUsed    int64

	Userdata interface{}
}

// ResetAfter returns the current estimated duration until this bucket resets.
func (b *Bucket) ResetAfter() time.Duration {
	wait := time.Until(b.reset)
	if wait < 0 {
		return 0
	}
	return wait
}

// Release unlocks the bucket and applies Discord's response headers.
func (b *Bucket) Release(headers http.Header) error {
	defer b.Unlock()
	atomic.StoreInt64(&b.lastUsed, time.Now().UnixNano())

	if headers == nil {
		return nil
	}

	remaining := headers.Get("X-RateLimit-Remaining")
	limit := headers.Get("X-RateLimit-Limit")
	reset := headers.Get("X-RateLimit-Reset")
	global := headers.Get("X-RateLimit-Global")
	resetAfter := headers.Get("X-RateLimit-Reset-After")
	bucketHash := headers.Get("X-RateLimit-Bucket")
	scope := RateLimitScope(strings.ToLower(headers.Get("X-RateLimit-Scope")))

	if bucketHash != "" && b.ratelimiter != nil {
		b.ratelimiter.registerBucketHash(b, bucketHash)
	}

	if scope != "" {
		b.Scope = scope
	}
	if limit != "" {
		parsedLimit, err := strconv.ParseInt(limit, 10, 32)
		if err != nil {
			return err
		}
		b.Limit = int(parsedLimit)
	}

	if resetAfter != "" {
		parsedAfter, err := strconv.ParseFloat(resetAfter, 64)
		if err != nil {
			return err
		}
		resetAt := time.Now().Add(floatSecondsDuration(parsedAfter))

		if headerIsTrue(global) || scope == RateLimitScopeGlobal {
			atomic.StoreInt64(b.global, resetAt.UnixNano())
		} else {
			b.reset = resetAt
		}
	} else if reset != "" {
		discordTime, err := http.ParseTime(headers.Get("Date"))
		if err != nil {
			return err
		}
		unix, err := strconv.ParseFloat(reset, 64)
		if err != nil {
			return err
		}

		whole, frac := math.Modf(unix)
		delta := time.Unix(int64(whole), 0).
			Add(time.Duration(frac*1000)*time.Millisecond).
			Sub(discordTime) + 250*time.Millisecond
		b.reset = time.Now().Add(delta)
	}

	if remaining != "" {
		parsedRemaining, err := strconv.ParseInt(remaining, 10, 32)
		if err != nil {
			return err
		}
		b.Remaining = int(parsedRemaining)
	}
	return nil
}

func (r *RateLimiter) registerBucketHash(bucket *Bucket, hash string) {
	now := time.Now()
	origin := bucket.Key
	if bucket.RouteKey != "" {
		origin = rateLimitRouteBucketKey(bucket.RouteKey, bucket.MajorKey)
	}
	target := rateLimitDiscoveredBucketKey(hash, bucket.MajorKey)

	r.Lock()
	defer r.Unlock()
	r.cleanupBucketsLocked(now)
	if existing, ok := r.buckets[target]; ok {
		r.setBucketHashLocked(origin, target, now)
		atomic.StoreInt64(&existing.lastUsed, now.UnixNano())
		if current, ok := r.buckets[origin]; ok && current == bucket && existing != bucket {
			delete(r.buckets, origin)
		}
		return
	}

	if current, ok := r.buckets[origin]; ok && current == bucket {
		delete(r.buckets, origin)
	}
	r.makeBucketRoomLocked(1)
	r.buckets[target] = bucket
	r.setBucketHashLocked(origin, target, now)
}

// ApplyGlobalLimit records a global deadline learned from a 429 body.
func (r *RateLimiter) ApplyGlobalLimit(retryAfter time.Duration) {
	if retryAfter <= 0 {
		return
	}
	deadline := time.Now().Add(retryAfter).UnixNano()
	for {
		current := atomic.LoadInt64(r.global)
		if current >= deadline || atomic.CompareAndSwapInt64(r.global, current, deadline) {
			return
		}
	}
}

// RecordResponse adds an invalid response to the rolling 401/403/429 budget.
func (r *RateLimiter) RecordResponse(statusCode int) InvalidRequestStatus {
	now := time.Now()
	r.Lock()
	defer r.Unlock()
	r.pruneInvalidRequestsLocked(now)
	if statusCode == http.StatusUnauthorized ||
		statusCode == http.StatusForbidden ||
		statusCode == http.StatusTooManyRequests {
		r.invalidRequests = append(r.invalidRequests, now)
	}
	return r.invalidRequestStatusLocked(now, true)
}

// InvalidRequestStatus returns a snapshot of the rolling invalid-request budget.
func (r *RateLimiter) InvalidRequestStatus() InvalidRequestStatus {
	now := time.Now()
	r.Lock()
	defer r.Unlock()
	r.pruneInvalidRequestsLocked(now)
	return r.invalidRequestStatusLocked(now, false)
}

// CheckInvalidRequestLimit returns an error while the circuit breaker is active.
func (r *RateLimiter) CheckInvalidRequestLimit() error {
	status := r.InvalidRequestStatus()
	if !status.Blocked {
		return nil
	}
	return InvalidRequestLimitError{
		Count:      status.Count,
		Limit:      status.Limit,
		ResetAfter: status.ResetAfter,
	}
}

func (r *RateLimiter) pruneInvalidRequestsLocked(now time.Time) {
	window := r.invalidRequestWindowLocked()
	cutoff := now.Add(-window)
	first := 0
	for first < len(r.invalidRequests) && !r.invalidRequests[first].After(cutoff) {
		first++
	}
	if first > 0 {
		copy(r.invalidRequests, r.invalidRequests[first:])
		r.invalidRequests = r.invalidRequests[:len(r.invalidRequests)-first]
	}
	if len(r.invalidRequests) < r.invalidRequestWarningLocked() {
		r.invalidWarned = false
	}
}

func (r *RateLimiter) invalidRequestStatusLocked(now time.Time, updateWarning bool) InvalidRequestStatus {
	count := len(r.invalidRequests)
	limit := r.InvalidRequestLimit
	warningThreshold := r.invalidRequestWarningLocked()
	status := InvalidRequestStatus{
		Count:            count,
		Limit:            limit,
		WarningThreshold: warningThreshold,
		Window:           r.invalidRequestWindowLocked(),
		Warning:          warningThreshold > 0 && count >= warningThreshold,
		Blocked:          limit > 0 && count >= limit,
	}
	if count > 0 {
		status.ResetAfter = time.Until(r.invalidRequests[0].Add(status.Window))
		if status.ResetAfter < 0 {
			status.ResetAfter = 0
		}
	}
	if updateWarning && status.Warning && !r.invalidWarned {
		status.WarningTriggered = true
		r.invalidWarned = true
	}
	return status
}

func (r *RateLimiter) invalidRequestWarningLocked() int {
	if r.InvalidRequestLimit <= 0 {
		return 0
	}
	threshold := r.InvalidRequestWarningThreshold
	if threshold <= 0 || threshold > r.InvalidRequestLimit {
		threshold = r.InvalidRequestLimit
	}
	return threshold
}

func (r *RateLimiter) invalidRequestWindowLocked() time.Duration {
	if r.InvalidRequestWindow <= 0 {
		return defaultInvalidRequestWindow
	}
	return r.InvalidRequestWindow
}

func headerIsTrue(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value != "" && value != "0" && value != "false"
}

func floatSecondsDuration(seconds float64) time.Duration {
	whole, frac := math.Modf(seconds)
	return time.Duration(whole)*time.Second + time.Duration(frac*1000)*time.Millisecond
}

func rateLimitRouteBucketKey(routeKey, majorKey string) string {
	return rateLimitRouteKeyPrefix + routeKey + "|" + rateLimitMajorKeyPrefix + majorKey
}

func rateLimitDiscoveredBucketKey(hash, majorKey string) string {
	return rateLimitDiscoveredBucketKeyPrefix + hash + "|" + rateLimitMajorKeyPrefix + majorKey
}

func restRateLimitKeys(method, rawURL, bucketID string) (routeKey, majorKey string) {
	if bucketID == "" {
		bucketID = rawURL
	}
	routePath := rateLimitPath(bucketID)
	actualPath := rateLimitPath(rawURL)
	routeKey = strings.ToUpper(method) + " " + normalizeRateLimitPath(routePath)
	majorKey = hashRateLimitMajorParameters(actualPath)
	return routeKey, majorKey
}

func rateLimitPath(value string) string {
	parsed, err := url.Parse(value)
	if err == nil && parsed.Path != "" {
		return parsed.Path
	}
	return strings.SplitN(value, "?", 2)[0]
}

func normalizeRateLimitPath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i := range segments {
		if segments[i] == "" {
			continue
		}
		if isSnowflakeSegment(segments[i]) {
			segments[i] = ":id"
		}
	}
	for i, segment := range segments {
		switch segment {
		case "webhooks", "interactions":
			if i+1 < len(segments) && segments[i+1] != "" {
				segments[i+1] = ":id"
			}
			if i+2 < len(segments) && segments[i+2] != "" {
				segments[i+2] = ":token"
			}
		}
	}
	if len(segments) == 1 && segments[0] == "" {
		return "/"
	}
	return "/" + strings.Join(segments, "/")
}

func hashRateLimitMajorParameters(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	major := make([]string, 0, 4)
	for i, segment := range segments {
		switch segment {
		case "channels", "guilds":
			if i+1 < len(segments) && segments[i+1] != "" {
				major = append(major, segment+"="+segments[i+1])
			}
		case "webhooks", "interactions":
			if i+1 < len(segments) && segments[i+1] != "" {
				major = append(major, segment+"="+segments[i+1])
			}
			if i+2 < len(segments) && segments[i+2] != "" {
				major = append(major, segment+"_token="+segments[i+2])
			}
		}
	}
	if len(major) == 0 {
		major = append(major, "none")
	}
	sum := sha256.Sum256([]byte(strings.Join(major, "\x00")))
	return hex.EncodeToString(sum[:])
}

func isSnowflakeSegment(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
