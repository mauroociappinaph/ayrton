package github

import (
	"context"
	"math"
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter per installation
type RateLimiter struct {
	mu           sync.RWMutex
	buckets      map[int64]*tokenBucket
	ratePerSec   float64 // tokens per second
	burst        int     // maximum bucket size
	hourlyLimit  int     // hourly limit (GitHub: 5000)
	window       time.Duration
	stopCh       chan struct{}
}

// tokenBucket represents a token bucket for a single installation
type tokenBucket struct {
	tokens     float64
	lastRefill time.Time
	ratePerSec float64
	burst      int
	mu         sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(ratePerSec float64, burst, hourlyLimit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		buckets:     make(map[int64]*tokenBucket),
		ratePerSec:  ratePerSec,
		burst:       burst,
		hourlyLimit: hourlyLimit,
		window:      window,
		stopCh:      make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

// NewDefaultRateLimiter creates a rate limiter with GitHub API defaults
// 4500 requests/hour (500 buffer from 5000 limit), burst of 100
func NewDefaultRateLimiter() *RateLimiter {
	return NewRateLimiter(4500.0/3600.0, 100, 4500, time.Hour)
}

// Allow checks if a request is allowed for the given installation
// Returns true if allowed, false if rate limited
func (rl *RateLimiter) Allow(ctx context.Context, installationID int64) (bool, error) {
	bucket := rl.getBucket(installationID)
	return bucket.take(1), nil
}

// WaitForToken blocks until a token is available or context is cancelled
func (rl *RateLimiter) WaitForToken(ctx context.Context, installationID int64) error {
	bucket := rl.getBucket(installationID)

	for {
		if bucket.take(1) {
			return nil
		}

		// Calculate time until next token
		waitTime := bucket.timeUntilTokens(1)
		
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
			// Continue loop to try again
		}
	}
}

// GetBackoff returns the exponential backoff duration for a retry attempt
// Attempt 1 = 1s, 2 = 2s, 3 = 4s, etc. Capped at 1 minute
func (rl *RateLimiter) GetBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	
	// Exponential backoff: 2^(attempt-1) seconds
	backoffSec := math.Pow(2, float64(attempt-1))
	backoff := time.Duration(backoffSec) * time.Second
	
	// Cap at 1 minute
	if backoff > time.Minute {
		return time.Minute
	}
	
	return backoff
}

// Reset resets the bucket for an installation
func (rl *RateLimiter) Reset(installationID int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, installationID)
}

// getBucket gets or creates a bucket for the installation
func (rl *RateLimiter) getBucket(installationID int64) *tokenBucket {
	rl.mu.RLock()
	bucket, ok := rl.buckets[installationID]
	rl.mu.RUnlock()

	if ok {
		return bucket
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if bucket, ok := rl.buckets[installationID]; ok {
		return bucket
	}

	bucket = &tokenBucket{
		tokens:     float64(rl.burst),
		lastRefill: time.Now(),
		ratePerSec: rl.ratePerSec,
		burst:      rl.burst,
	}
	rl.buckets[installationID] = bucket
	return bucket
}

// take attempts to take n tokens from the bucket
func (b *tokenBucket) take(n int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refill()

	if b.tokens >= float64(n) {
		b.tokens -= float64(n)
		return true
	}

	return false
}

// refill adds tokens based on elapsed time
func (b *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	
	// Add tokens based on rate
	b.tokens += elapsed * b.ratePerSec
	
	// Cap at burst
	if b.tokens > float64(b.burst) {
		b.tokens = float64(b.burst)
	}
	
	b.lastRefill = now
}

// timeUntilTokens returns the duration until n tokens are available
func (b *tokenBucket) timeUntilTokens(n int) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refill()

	if b.tokens >= float64(n) {
		return 0
	}

	needed := float64(n) - b.tokens
	seconds := needed / b.ratePerSec
	return time.Duration(seconds * float64(time.Second))
}

// cleanup periodically removes stale buckets
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for id, bucket := range rl.buckets {
				// Remove buckets that haven't been used in 2 windows
				bucket.mu.Lock()
				if now.Sub(bucket.lastRefill) > 2*rl.window {
					delete(rl.buckets, id)
				}
				bucket.mu.Unlock()
			}
			rl.mu.Unlock()
		}
	}
}

// Stop stops the cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

// RateLimitConfig holds rate limit configuration
type RateLimitConfig struct {
	RequestsPerHour int           `mapstructure:"requests_per_hour" yaml:"requests_per_hour"`
	Burst           int           `mapstructure:"burst" yaml:"burst"`
	Window          time.Duration `mapstructure:"window" yaml:"window"`
}

// DefaultRateLimitConfig returns the default rate limit configuration
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerHour: 4500,
		Burst:           100,
		Window:          time.Hour,
	}
}