package github

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateLimiter_Allow(t *testing.T) {
	// Create a limiter with 5 requests per second, burst of 10
	limiter := NewRateLimiter(5, 10, 5000, time.Hour)
	defer limiter.Stop()

	// Should allow burst up to capacity
	for i := 0; i < 10; i++ {
		allowed, err := limiter.Allow(context.Background(), 12345)
		require.NoError(t, err)
		require.True(t, allowed, "request %d should be allowed", i)
	}

	// Next request should be denied (or wait)
	allowed, err := limiter.Allow(context.Background(), 12345)
	require.NoError(t, err)
	require.False(t, allowed, "request 11 should be denied")
}

func TestRateLimiter_PerInstallation(t *testing.T) {
	limiter := NewRateLimiter(5, 10, 5000, time.Hour)
	defer limiter.Stop()

	// Use up installation 1's quota
	for i := 0; i < 10; i++ {
		allowed, _ := limiter.Allow(context.Background(), 1)
		require.True(t, allowed)
	}

	// Installation 2 should have its own quota
	for i := 0; i < 10; i++ {
		allowed, _ := limiter.Allow(context.Background(), 2)
		require.True(t, allowed)
	}
}

func TestRateLimiter_Refill(t *testing.T) {
	// 10 requests per second, burst of 5
	limiter := NewRateLimiter(10, 5, 5000, time.Hour)
	defer limiter.Stop()

	// Use all 5 tokens
	for i := 0; i < 5; i++ {
		allowed, _ := limiter.Allow(context.Background(), 1)
		require.True(t, allowed)
	}

	// Should be denied now
	allowed, _ := limiter.Allow(context.Background(), 1)
	require.False(t, allowed)

	// Wait for refill (100ms should give ~1 token at 10/sec)
	time.Sleep(150 * time.Millisecond)

	// Should allow at least 1 more
	allowed, _ = limiter.Allow(context.Background(), 1)
	require.True(t, allowed)
}

func TestRateLimiter_ExponentialBackoff(t *testing.T) {
	limiter := NewRateLimiter(5, 5, 5000, time.Hour)
	defer limiter.Stop()

	// Use all tokens
	for i := 0; i < 5; i++ {
		allowed, _ := limiter.Allow(context.Background(), 1)
		require.True(t, allowed)
	}

	// Get backoff time - should increase with each retry
	backoff1 := limiter.GetBackoff(1)
	backoff2 := limiter.GetBackoff(2)
	backoff3 := limiter.GetBackoff(3)

	require.Less(t, backoff1, backoff2)
	require.Less(t, backoff2, backoff3)

	// Max backoff should be capped at 1 minute
	maxBackoff := limiter.GetBackoff(10)
	require.LessOrEqual(t, maxBackoff, time.Minute)
}

func TestRateLimiter_ThreadSafety(t *testing.T) {
	limiter := NewRateLimiter(100, 200, 5000, time.Hour)
	defer limiter.Stop()

	done := make(chan bool, 20)
	
	// Concurrent access from multiple goroutines
	for i := 0; i < 20; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				limiter.Allow(context.Background(), 1)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// Should not panic or deadlock
	require.True(t, true)
}

func TestRateLimiter_Reset(t *testing.T) {
	limiter := NewRateLimiter(5, 10, 5000, time.Hour)
	defer limiter.Stop()

	// Use some tokens
	for i := 0; i < 5; i++ {
		allowed, _ := limiter.Allow(context.Background(), 1)
		require.True(t, allowed)
	}

	// Reset the limiter for this installation
	limiter.Reset(1)

	// Should have full quota again
	for i := 0; i < 10; i++ {
		allowed, _ := limiter.Allow(context.Background(), 1)
		require.True(t, allowed)
	}
}

func TestRateLimiter_DefaultLimits(t *testing.T) {
	// GitHub API limit: 5000 requests/hour per installation
	// We use 4500 with 500 buffer
	limiter := NewDefaultRateLimiter()
	defer limiter.Stop()

	// Should allow up to 4500/hour
	// Burst should be reasonable (e.g., 100)
	for i := 0; i < 100; i++ {
		allowed, _ := limiter.Allow(context.Background(), 1)
		require.True(t, allowed)
	}
}

func TestRateLimiter_WaitForToken(t *testing.T) {
	limiter := NewRateLimiter(10, 5, 5000, time.Hour)
	defer limiter.Stop()

	// Use all tokens
	for i := 0; i < 5; i++ {
		allowed, _ := limiter.Allow(context.Background(), 1)
		require.True(t, allowed)
	}

	// Wait for token with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := limiter.WaitForToken(ctx, 1)
	require.NoError(t, err) // Should get token within 500ms (refill at 10/sec)
}