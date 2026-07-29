package dgo

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimiter holds all ratelimit buckets
type RateLimiter struct {
	sync.Mutex
	global       *int64
	buckets      map[string]*Bucket
	bucketHashes map[string]string // maps endpoint key to bucket hash
}

// NewRatelimiter returns a new RateLimiter
func NewRatelimiter() *RateLimiter {

	return &RateLimiter{
		buckets:      make(map[string]*Bucket),
		bucketHashes: make(map[string]string),
		global:       new(int64),
	}
}

// GetBucket retrieves or creates a bucket
func (r *RateLimiter) GetBucket(key string) *Bucket {
	r.Lock()
	defer r.Unlock()

	// Check if this key maps to a known bucket hash
	if hash, ok := r.bucketHashes[key]; ok {
		if bucket, ok := r.buckets[hash]; ok {
			return bucket
		}
	}

	if bucket, ok := r.buckets[key]; ok {
		return bucket
	}

	b := &Bucket{
		Remaining:   1,
		Key:         key,
		global:      r.global,
		ratelimiter: r,
	}

	r.buckets[key] = b
	return b
}

// GetWaitTime returns the duration you should wait for a Bucket
func (r *RateLimiter) GetWaitTime(b *Bucket, minRemaining int) time.Duration {
	now := time.Now()
	var wait time.Duration

	// If we ran out of calls and the reset time is still ahead of us
	// then we need to take it easy and relax a little
	if b.Remaining < minRemaining && b.reset.After(now) {
		wait = b.reset.Sub(now)
	}

	// Check for global ratelimits
	sleepTo := time.Unix(0, atomic.LoadInt64(r.global))
	if globalWait := sleepTo.Sub(now); globalWait > wait {
		wait = globalWait
	}

	return wait
}

// LockBucket Locks until a request can be made
func (r *RateLimiter) LockBucket(bucketID string) *Bucket {
	// Fallback to background context for backward compatibility
	b, _ := r.LockBucketContext(context.Background(), bucketID)
	return b
}

// LockBucketContext Locks until a request can be made or context is cancelled
func (r *RateLimiter) LockBucketContext(ctx context.Context, bucketID string) (*Bucket, error) {
	return r.LockBucketObjectContext(ctx, r.GetBucket(bucketID))
}

// LockBucketObject Locks an already resolved bucket until a request can be made
func (r *RateLimiter) LockBucketObject(b *Bucket) *Bucket {
	// Fallback to background context for backward compatibility
	b, _ = r.LockBucketObjectContext(context.Background(), b)
	return b
}

// LockBucketObjectContext Locks an already resolved bucket until a request can be made or context is cancelled
func (r *RateLimiter) LockBucketObjectContext(ctx context.Context, b *Bucket) (*Bucket, error) {
	b.Lock()

	for {
		wait := r.GetWaitTime(b, 1)
		if wait <= 0 {
			break
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
	return b, nil
}

// Bucket represents a ratelimit bucket, each bucket gets ratelimited individually (-global ratelimits)
type Bucket struct {
	sync.Mutex
	Key         string
	Remaining   int
	reset       time.Time
	global      *int64
	ratelimiter *RateLimiter

	Userdata interface{}
}

// Release unlocks the bucket and reads the headers to update the buckets ratelimit info
// and locks up the whole thing in case if there's a global ratelimit.
func (b *Bucket) Release(headers http.Header) error {
	defer b.Unlock()

	if headers == nil {
		return nil
	}

	remaining := headers.Get("X-RateLimit-Remaining")
	reset := headers.Get("X-RateLimit-Reset")
	global := headers.Get("X-RateLimit-Global")
	resetAfter := headers.Get("X-RateLimit-Reset-After")
	bucketHash := headers.Get("X-RateLimit-Bucket")

	// Register the bucket hash if provided by Discord
	if bucketHash != "" && b.ratelimiter != nil {
		b.ratelimiter.Lock()
		b.ratelimiter.bucketHashes[b.Key] = bucketHash
		// Also register the bucket under the hash key if not already
		if _, ok := b.ratelimiter.buckets[bucketHash]; !ok {
			b.ratelimiter.buckets[bucketHash] = b
		}
		b.ratelimiter.Unlock()
	}

	// Update global and per bucket reset time if the proper headers are available
	// If global is set, then it will block all buckets until after Retry-After
	// If Retry-After without global is provided it will use that for the new reset
	// time since it's more accurate than X-RateLimit-Reset.
	// If Retry-After after is not proided, it will update the reset time from X-RateLimit-Reset
	if resetAfter != "" {
		parsedAfter, err := strconv.ParseFloat(resetAfter, 64)
		if err != nil {
			return err
		}

		whole, frac := math.Modf(parsedAfter)
		resetAt := time.Now().Add(time.Duration(whole) * time.Second).Add(time.Duration(frac*1000) * time.Millisecond)

		// Lock either this single bucket or all buckets
		if global != "" {
			atomic.StoreInt64(b.global, resetAt.UnixNano())
		} else {
			b.reset = resetAt
		}
	} else if reset != "" {
		// Calculate the reset time by using the date header returned from discord
		discordTime, err := http.ParseTime(headers.Get("Date"))
		if err != nil {
			return err
		}

		unix, err := strconv.ParseFloat(reset, 64)
		if err != nil {
			return err
		}

		// Calculate the time until reset and add it to the current local time
		// some extra time is added because without it i still encountered 429's.
		// The added amount is the lowest amount that gave no 429's
		// in 1k requests
		whole, frac := math.Modf(unix)
		delta := time.Unix(int64(whole), 0).Add(time.Duration(frac*1000)*time.Millisecond).Sub(discordTime) + time.Millisecond*250
		b.reset = time.Now().Add(delta)
	}

	// Update remaining if header is present
	if remaining != "" {
		parsedRemaining, err := strconv.ParseInt(remaining, 10, 32)
		if err != nil {
			return err
		}
		b.Remaining = int(parsedRemaining)
	}

	return nil
}
