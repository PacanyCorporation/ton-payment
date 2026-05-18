package blockchain

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/tl"
	"github.com/xssnick/tonutils-go/ton"
	"golang.org/x/time/rate"
)

// Retryable error codes that should trigger node failover
var retryableCodes = map[int]struct{}{
	500:  {}, // failed to resolve block
	502:  {}, // backend node timeout
	651:  {}, // block not in DB
	652:  {}, // block not applied
	-400: {}, // error
	-503: {}, // error
}

type limitedLiteClient struct {
	limiter  *rate.Limiter
	original ton.LiteClient
}

func newLimitedClient(lc ton.LiteClient, rateLimit int) *limitedLiteClient {
	return &limitedLiteClient{
		original: lc,
		limiter:  rate.NewLimiter(rate.Limit(rateLimit), 1),
	}
}

func (w *limitedLiteClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	err := w.limiter.Wait(ctx)
	if err != nil {
		return fmt.Errorf("limiter err: %w", err)
	}
	return w.original.QueryLiteserver(ctx, payload, result)
}

func (w *limitedLiteClient) StickyContext(ctx context.Context) context.Context {
	return w.original.StickyContext(ctx)
}

func (w *limitedLiteClient) StickyNodeID(ctx context.Context) uint32 {
	return w.original.StickyNodeID(ctx)
}

func (w *limitedLiteClient) StickyContextNextNode(ctx context.Context) (context.Context, error) {
	return w.original.StickyContextNextNode(ctx)
}

func (w *limitedLiteClient) StickyContextNextNodeBalanced(ctx context.Context) (context.Context, error) {
	return w.original.StickyContextNextNodeBalanced(ctx)
}

// retryableLiteClient wraps limitedLiteClient with retry logic for specific error codes
type retryableLiteClient struct {
	*limitedLiteClient
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func newRetryableClient(lc ton.LiteClient, rateLimit, maxRetries int) *retryableLiteClient {
	return &retryableLiteClient{
		limitedLiteClient: newLimitedClient(lc, rateLimit),
		maxRetries:        maxRetries,
		baseDelay:         100 * time.Millisecond,
		maxDelay:          5 * time.Second,
	}
}

func (w *retryableLiteClient) QueryLiteserver(ctx context.Context, payload tl.Serializable, result tl.Serializable) error {
	var lastErr error
	delay := w.baseDelay

	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		lastErr = w.limitedLiteClient.QueryLiteserver(ctx, payload, result)
		if lastErr == nil {
			return nil
		}

		if !isRetryableError(lastErr) {
			return lastErr
		}

		if attempt == w.maxRetries {
			break
		}

		// Switch to next node before retry
		ctx, err := w.StickyContextNextNode(ctx)
		if err != nil {
			return fmt.Errorf("failed to switch node: %w", lastErr)
		}

		// Exponential backoff
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		delay = time.Duration(math.Min(float64(delay*2), float64(w.maxDelay)))
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	errStr := err.Error()

	// Check for LSError codes
	if strings.Contains(errStr, "code 500") ||
		strings.Contains(errStr, "code 502") ||
		strings.Contains(errStr, "code 651") ||
		strings.Contains(errStr, "code 652") ||
		strings.Contains(errStr, "code -400") ||
		strings.Contains(errStr, "code -503") {
		return true
	}

	// Check for "Failed to get account state" (handled by native retry but we catch it too)
	if strings.Contains(errStr, "Failed to get account state") {
		return true
	}

	// Check for context deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	return false
}
