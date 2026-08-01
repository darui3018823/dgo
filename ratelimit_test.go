package dgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRateLimiterUsesLongestRouteOrGlobalWait(t *testing.T) {
	rl := NewRatelimiter()
	bucket := rl.GetBucket("/channels/1/messages")
	now := time.Now()
	bucket.Remaining = 0
	bucket.reset = now.Add(time.Second)
	atomic.StoreInt64(rl.global, now.Add(5*time.Second).UnixNano())

	wait := rl.GetWaitTime(bucket, 1)
	if wait < 4*time.Second || wait > 5*time.Second {
		t.Fatalf("wait = %s, want global wait near 5s", wait)
	}
}

func TestRateLimiterReevaluatesWaitAfterWake(t *testing.T) {
	rl := NewRatelimiter()
	bucket := rl.GetBucket("/channels/1/messages")
	bucket.Remaining = 0
	bucket.reset = time.Now().Add(30 * time.Millisecond)

	started := time.Now()
	result := make(chan error, 1)
	go func() {
		locked, err := rl.LockBucketObjectContext(context.Background(), bucket)
		if err == nil {
			err = locked.Release(nil)
		}
		result <- err
	}()

	time.Sleep(10 * time.Millisecond)
	atomic.StoreInt64(rl.global, time.Now().Add(90*time.Millisecond).UnixNano())

	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 80*time.Millisecond {
		t.Fatalf("bucket lock returned after %s without re-evaluating global wait", elapsed)
	}
}

func TestReactionBucketsUseDiscordHeaders(t *testing.T) {
	rl := NewRatelimiter()
	bucket := rl.LockBucket("/channels/1/messages/2/reactions/")
	headers := http.Header{
		"X-Ratelimit-Remaining":   {"0"},
		"X-Ratelimit-Reset-After": {"0.1"},
	}
	if err := bucket.Release(headers); err != nil {
		t.Fatal(err)
	}

	bucket.Lock()
	defer bucket.Unlock()
	if bucket.Remaining != 0 {
		t.Fatalf("reaction bucket remaining = %d, want 0 from Discord header", bucket.Remaining)
	}
	if wait := rl.GetWaitTime(bucket, 1); wait <= 0 {
		t.Fatalf("reaction bucket wait = %s, want positive duration", wait)
	}
}

func TestRESTRateLimitKeysSeparateMajorParametersAndRedactTokens(t *testing.T) {
	routeOne, majorOne := restRateLimitKeys(
		http.MethodGet,
		"https://discord.com/api/v10/channels/111/messages/222",
		"https://discord.com/api/v10/channels/111/messages/",
	)
	routeTwo, majorTwo := restRateLimitKeys(
		http.MethodGet,
		"https://discord.com/api/v10/channels/333/messages/444",
		"https://discord.com/api/v10/channels/333/messages/",
	)
	if routeOne != routeTwo {
		t.Fatalf("normalized route keys differ: %q != %q", routeOne, routeTwo)
	}
	if majorOne == majorTwo {
		t.Fatal("different channel major parameters produced the same key")
	}

	const webhookToken = "super-secret-webhook-token"
	webhookRoute, webhookMajor := restRateLimitKeys(
		http.MethodPost,
		"https://discord.com/api/v10/webhooks/555/"+webhookToken,
		"https://discord.com/api/v10/webhooks/555/"+webhookToken,
	)
	for name, value := range map[string]string{
		"route": webhookRoute,
		"major": webhookMajor,
	} {
		if strings.Contains(value, webhookToken) {
			t.Fatalf("%s key retained webhook token: %q", name, value)
		}
	}
	if !strings.Contains(webhookRoute, ":token") {
		t.Fatalf("webhook route = %q, want normalized token placeholder", webhookRoute)
	}
}

func TestRateLimiterDiscoveredBucketsKeepMajorParametersSeparate(t *testing.T) {
	rl := NewRatelimiter()
	rl.GlobalRateLimit = 0
	headers := make(http.Header)
	headers.Set("X-RateLimit-Bucket", "shared-discord-hash")
	headers.Set("X-RateLimit-Remaining", "1")

	first, err := rl.LockBucketRouteContext(context.Background(), "GET /channels/:id/messages", "major-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(headers); err != nil {
		t.Fatal(err)
	}
	second, err := rl.LockBucketRouteContext(context.Background(), "GET /channels/:id/messages", "major-two")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Release(headers); err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("Discord bucket hash was shared across different major parameters")
	}

	firstAgain, err := rl.LockBucketRouteContext(context.Background(), "GET /channels/:id/messages", "major-one")
	if err != nil {
		t.Fatal(err)
	}
	if firstAgain != first {
		t.Fatal("same route and major parameter did not reuse discovered bucket")
	}
	if err := firstAgain.Release(headers); err != nil {
		t.Fatal(err)
	}

	alias, err := rl.LockBucketRouteContext(context.Background(), "POST /channels/:id/messages", "major-one")
	if err != nil {
		t.Fatal(err)
	}
	if err := alias.Release(headers); err != nil {
		t.Fatal(err)
	}
	aliasAgain, err := rl.LockBucketRouteContext(context.Background(), "POST /channels/:id/messages", "major-one")
	if err != nil {
		t.Fatal(err)
	}
	if aliasAgain != first {
		t.Fatal("routes with the same discovered hash and major did not converge")
	}
	if err := aliasAgain.Release(nil); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimiterCanonicalBucketReceivesAliasResponse(t *testing.T) {
	rl := NewRatelimiter()
	rl.GlobalRateLimit = 0

	firstHeaders := make(http.Header)
	firstHeaders.Set("X-RateLimit-Bucket", "shared-discord-hash")
	firstHeaders.Set("X-RateLimit-Remaining", "5")
	first, err := rl.LockBucketRouteContext(
		context.Background(),
		"GET /channels/:id/messages",
		"same-major",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(firstHeaders); err != nil {
		t.Fatal(err)
	}

	aliasHeaders := make(http.Header)
	aliasHeaders.Set("X-RateLimit-Bucket", "shared-discord-hash")
	aliasHeaders.Set("X-RateLimit-Remaining", "0")
	aliasHeaders.Set("X-RateLimit-Reset-After", "0.2")
	alias, err := rl.LockBucketRouteContext(
		context.Background(),
		"POST /channels/:id/messages",
		"same-major",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := alias.Release(aliasHeaders); err != nil {
		t.Fatal(err)
	}

	first.Lock()
	remaining := first.Remaining
	resetAfter := first.ResetAfter()
	first.Unlock()
	if remaining != 0 {
		t.Fatalf("canonical bucket remaining = %d, want 0", remaining)
	}
	if resetAfter <= 0 {
		t.Fatalf("canonical bucket reset after = %s, want positive duration", resetAfter)
	}
}

func TestRateLimiterBoundsBucketCache(t *testing.T) {
	rl := NewRatelimiter()
	rl.MaxBuckets = 2
	rl.GetBucket("one")
	time.Sleep(time.Millisecond)
	rl.GetBucket("two")
	time.Sleep(time.Millisecond)
	rl.GetBucket("three")

	rl.Lock()
	if got := len(rl.buckets); got != 2 {
		rl.Unlock()
		t.Fatalf("bucket cache size = %d, want 2", got)
	}
	if _, ok := rl.buckets["one"]; ok {
		rl.Unlock()
		t.Fatal("oldest bucket was not evicted")
	}
	rl.Unlock()

	ttlLimiter := NewRatelimiter()
	ttlLimiter.BucketTTL = time.Millisecond
	stale := ttlLimiter.GetBucket("stale")
	atomic.StoreInt64(&stale.lastUsed, time.Now().Add(-time.Second).UnixNano())
	ttlLimiter.Lock()
	ttlLimiter.lastCleanup = time.Time{}
	ttlLimiter.Unlock()
	ttlLimiter.GetBucket("fresh")
	ttlLimiter.Lock()
	_, retained := ttlLimiter.buckets["stale"]
	ttlLimiter.Unlock()
	if retained {
		t.Fatal("expired bucket remained in cache")
	}

	mappingLimiter := NewRatelimiter()
	mappingLimiter.GlobalRateLimit = 0
	mappingLimiter.MaxBucketMappings = 2
	hashHeaders := make(http.Header)
	hashHeaders.Set("X-RateLimit-Bucket", "one-discovered-bucket")
	hashHeaders.Set("X-RateLimit-Remaining", "1")
	for _, route := range []string{"GET /one", "GET /two", "GET /three"} {
		bucket, err := mappingLimiter.LockBucketRouteContext(context.Background(), route, "same-major")
		if err != nil {
			t.Fatal(err)
		}
		if err := bucket.Release(hashHeaders); err != nil {
			t.Fatal(err)
		}
	}
	mappingLimiter.Lock()
	mappingCount := len(mappingLimiter.bucketHashes)
	mappingLimiter.Unlock()
	if mappingCount > 2 {
		t.Fatalf("bucket mapping cache size = %d, want at most 2", mappingCount)
	}
}

func TestRateLimiterProactivelyEnforcesGlobalBudget(t *testing.T) {
	rl := NewRatelimiter()
	rl.GlobalRateLimit = 2
	rl.GlobalRateWindow = 50 * time.Millisecond

	started := time.Now()
	for _, key := range []string{"one", "two", "three"} {
		bucket, err := rl.LockBucketContext(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		if err := bucket.Release(nil); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatalf("three requests passed a two-request global window in %s", elapsed)
	}
}

func TestRateLimiterTracksScopeAndInvalidRequestBudget(t *testing.T) {
	rl := NewRatelimiter()
	rl.GlobalRateLimit = 0
	bucket := rl.LockBucket("scope")
	headers := make(http.Header)
	headers.Set("X-RateLimit-Scope", "shared")
	headers.Set("X-RateLimit-Limit", "7")
	headers.Set("X-RateLimit-Remaining", "6")
	if err := bucket.Release(headers); err != nil {
		t.Fatal(err)
	}
	if bucket.Scope != RateLimitScopeShared || bucket.Limit != 7 || bucket.Remaining != 6 {
		t.Fatalf(
			"bucket metadata = scope %q, limit %d, remaining %d",
			bucket.Scope,
			bucket.Limit,
			bucket.Remaining,
		)
	}

	rl.InvalidRequestLimit = 3
	rl.InvalidRequestWarningThreshold = 2
	rl.InvalidRequestWindow = 30 * time.Millisecond
	if status := rl.RecordResponse(http.StatusUnauthorized); status.Count != 1 || status.Warning {
		t.Fatalf("first invalid request status = %+v", status)
	}
	if status := rl.RecordResponse(http.StatusForbidden); !status.Warning || !status.WarningTriggered {
		t.Fatalf("warning status = %+v", status)
	}
	if status := rl.RecordResponse(http.StatusTooManyRequests); !status.Blocked || status.Count != 3 {
		t.Fatalf("blocked status = %+v", status)
	}
	if err := rl.CheckInvalidRequestLimit(); !errors.Is(err, ErrInvalidRequestLimit) {
		t.Fatalf("circuit breaker error = %v", err)
	}

	time.Sleep(40 * time.Millisecond)
	if status := rl.InvalidRequestStatus(); status.Count != 0 || status.Blocked {
		t.Fatalf("expired invalid request status = %+v", status)
	}
	if err := rl.CheckInvalidRequestLimit(); err != nil {
		t.Fatalf("expired circuit breaker remained active: %v", err)
	}
}

func TestTooManyRequestsIncludesGlobalScope(t *testing.T) {
	var rateLimit TooManyRequests
	if err := Unmarshal([]byte(`{"message":"slow down","retry_after":0.25,"global":true}`), &rateLimit); err != nil {
		t.Fatal(err)
	}
	if !rateLimit.Global || rateLimit.RetryAfter != 250*time.Millisecond {
		t.Fatalf("decoded rate limit = %+v", rateLimit)
	}
}

// This test takes ~2 seconds to run
func TestRatelimitReset(t *testing.T) {
	rl := NewRatelimiter()

	sendReq := func(endpoint string) {
		bucket := rl.LockBucket(endpoint)

		headers := http.Header(make(map[string][]string))

		headers.Set("X-RateLimit-Remaining", "0")
		// Reset for approx 2 seconds from now
		headers.Set("X-RateLimit-Reset", fmt.Sprint(float64(time.Now().Add(time.Second*2).UnixNano())/1e9))
		headers.Set("Date", time.Now().Format(time.RFC850))

		err := bucket.Release(headers)
		if err != nil {
			t.Errorf("Release returned error: %v", err)
		}
	}

	sent := time.Now()
	sendReq("/guilds/99/channels")
	sendReq("/guilds/55/channels")
	sendReq("/guilds/66/channels")

	sendReq("/guilds/99/channels")
	sendReq("/guilds/55/channels")
	sendReq("/guilds/66/channels")

	// We hit the same endpoint 2 times, so we should only be ratelimited 2 second
	// And always less than 4 seconds (unless you're on a stoneage computer or using swap or something...)
	if time.Since(sent) >= time.Second && time.Since(sent) < time.Second*4 {
		t.Log("OK", time.Since(sent))
	} else {
		t.Error("Did not ratelimit correctly, got:", time.Since(sent))
	}
}

// This test takes ~1 seconds to run
func TestRatelimitGlobal(t *testing.T) {
	rl := NewRatelimiter()

	sendReq := func(endpoint string) {
		bucket := rl.LockBucket(endpoint)

		headers := http.Header(make(map[string][]string))

		headers.Set("X-RateLimit-Global", "1")
		// Reset for approx 1 seconds from now
		headers.Set("X-RateLimit-Reset-After", "1")

		err := bucket.Release(headers)
		if err != nil {
			t.Errorf("Release returned error: %v", err)
		}
	}

	sent := time.Now()

	// This should trigger a global ratelimit
	sendReq("/guilds/99/channels")
	time.Sleep(time.Millisecond * 100)

	// This shouldn't go through in less than 1 second
	sendReq("/guilds/55/channels")

	if time.Since(sent) >= time.Second && time.Since(sent) < time.Second*2 {
		t.Log("OK", time.Since(sent))
	} else {
		t.Error("Did not ratelimit correctly, got:", time.Since(sent))
	}
}

func BenchmarkRatelimitSingleEndpoint(b *testing.B) {
	rl := NewRatelimiter()
	for i := 0; i < b.N; i++ {
		sendBenchReq("/guilds/99/channels", rl)
	}
}

func BenchmarkRatelimitParallelMultiEndpoints(b *testing.B) {
	rl := NewRatelimiter()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sendBenchReq("/guilds/"+strconv.Itoa(i)+"/channels", rl)
			i++
		}
	})
}

// Does not actually send requests, but locks the bucket and releases it with made-up headers
func sendBenchReq(endpoint string, rl *RateLimiter) {
	bucket := rl.LockBucket(endpoint)

	headers := http.Header(make(map[string][]string))

	headers.Set("X-RateLimit-Remaining", "10")
	headers.Set("X-RateLimit-Reset", fmt.Sprint(float64(time.Now().UnixNano())/1e9))
	headers.Set("Date", time.Now().Format(time.RFC850))

	bucket.Release(headers)
}
