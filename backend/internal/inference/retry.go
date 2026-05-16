package inference

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"syscall"
	"time"
)

// RetryConfig controls the retry behavior for transient errors.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig is a reasonable default for model provider calls.
var DefaultRetryConfig = RetryConfig{
	MaxRetries: 3,
	BaseDelay:  500 * time.Millisecond,
	MaxDelay:   30 * time.Second,
}

// RetryingProvider wraps a Provider with exponential backoff retry logic.
// It only retries on transient errors (network issues, rate limits, server errors).
type RetryingProvider struct {
	inner  Provider
	config RetryConfig
}

// NewRetryingProvider wraps the given provider with retry logic.
func NewRetryingProvider(inner Provider, config RetryConfig) *RetryingProvider {
	if config.MaxRetries <= 0 {
		config.MaxRetries = DefaultRetryConfig.MaxRetries
	}
	if config.BaseDelay <= 0 {
		config.BaseDelay = DefaultRetryConfig.BaseDelay
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = DefaultRetryConfig.MaxDelay
	}
	return &RetryingProvider{
		inner:  inner,
		config: config,
	}
}

// Infer implements Provider. On transient errors, it retries with exponential backoff.
func (r *RetryingProvider) Infer(ctx context.Context, payload ContextPayload) (<-chan StreamingChunk, error) {
	var lastErr error
	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt, r.config.BaseDelay, r.config.MaxDelay)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		ch, err := r.inner.Infer(ctx, payload)
		if err == nil {
			return ch, nil
		}

		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("after %d attempts: %w", r.config.MaxRetries+1, lastErr)
}

// backoff computes exponential backoff with full jitter.
func backoff(attempt int, base, max time.Duration) time.Duration {
	d := base * (1 << (attempt - 1))
	if d > max {
		d = max
	}
	if d <= 0 {
		return base
	}
	// Full jitter: random value in [0, d)
	return time.Duration(rand.Int63n(int64(d)))
}

// isRetryable returns true for transient errors that are worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context cancellation is not retryable.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// HTTP status codes
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusTooManyRequests, // 429
			http.StatusBadGateway,         // 502
			http.StatusServiceUnavailable, // 503
			http.StatusGatewayTimeout:     // 504
			return true
		default:
			return false
		}
	}

	// Network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	// Connection refused, reset, etc.
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}

	return false
}

// httpError is a wrapper that carries an HTTP status code.
type httpError struct {
	StatusCode int
	Message    string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// newHTTPError creates an httpError for the given status code and message.
func newHTTPError(statusCode int, message string) error {
	return &httpError{StatusCode: statusCode, Message: message}
}
