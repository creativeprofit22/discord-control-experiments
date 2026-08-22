package discord

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rateBucket struct {
	turn             chan struct{}
	mu               sync.Mutex
	limit, remaining int
	reset            time.Time
	scope            string
	hash             string
}
type rateLimitCoordinator struct {
	mu     sync.Mutex
	clock  clock
	routes map[string]*rateBucket
	hashes map[string]*rateBucket
	global time.Time
}
type bucketLease struct {
	coordinator        *rateLimitCoordinator
	bucket             *rateBucket
	provisional, major string
	released           bool
}

func newRateLimitCoordinator(clock clock) *rateLimitCoordinator {
	return &rateLimitCoordinator{clock: clock, routes: make(map[string]*rateBucket), hashes: make(map[string]*rateBucket)}
}
func (c *rateLimitCoordinator) Acquire(ctx context.Context, method, template, major string) (*bucketLease, error) {
	provisional := method + " " + template + " " + major
	for {
		c.mu.Lock()
		global := c.global
		bucket := c.routes[provisional]
		if bucket == nil {
			bucket = &rateBucket{turn: make(chan struct{}, 1), remaining: 1}
			bucket.turn <- struct{}{}
			c.routes[provisional] = bucket
		}
		c.mu.Unlock()
		if wait := global.Sub(c.clock.Now()); wait > 0 {
			if err := c.clock.Sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-bucket.turn:
		}
		bucket.mu.Lock()
		wait := time.Duration(0)
		if bucket.remaining <= 0 {
			wait = bucket.reset.Sub(c.clock.Now())
		}
		bucket.mu.Unlock()
		if wait > 0 {
			if err := c.clock.Sleep(ctx, wait); err != nil {
				bucket.turn <- struct{}{}
				return nil, err
			}
		}
		return &bucketLease{coordinator: c, bucket: bucket, provisional: provisional, major: major}, nil
	}
}
func (l *bucketLease) Release() {
	if l == nil || l.released {
		return
	}
	l.released = true
	l.bucket.turn <- struct{}{}
}
func (l *bucketLease) Update(headers http.Header, status int) error {
	c := l.coordinator
	b := l.bucket
	now := c.clock.Now()
	hash := strings.TrimSpace(headers.Get("X-RateLimit-Bucket"))
	scope := strings.ToLower(strings.TrimSpace(headers.Get("X-RateLimit-Scope")))
	limit, limitOK, err := parseNonnegativeInt(headers.Get("X-RateLimit-Limit"))
	if err != nil {
		return err
	}
	remaining, remainingOK, err := parseNonnegativeInt(headers.Get("X-RateLimit-Remaining"))
	if err != nil {
		return err
	}
	reset, resetOK, err := parseReset(headers, now)
	if err != nil {
		return err
	}
	if status == http.StatusTooManyRequests {
		retry, retryOK, err := parseRetryAfter(headers.Get("Retry-After"), now)
		if err != nil {
			return err
		}
		if !retryOK {
			return fmt.Errorf("429 missing Retry-After")
		}
		reset = retry
		resetOK = true
		remaining = 0
		remainingOK = true
	}
	b.mu.Lock()
	if limitOK {
		b.limit = limit
	}
	if remainingOK {
		b.remaining = remaining
	}
	if resetOK {
		b.reset = reset
	}
	if scope != "" {
		b.scope = scope
	}
	if hash != "" {
		b.hash = hash
	}
	b.mu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if hash != "" {
		key := hash + " " + l.major
		if canonical := c.hashes[key]; canonical != nil && canonical != b {
			canonical.mu.Lock()
			b.mu.Lock()
			if b.reset.After(canonical.reset) {
				canonical.reset = b.reset
			}
			if b.remaining < canonical.remaining || canonical.remaining == 0 {
				canonical.remaining = b.remaining
			}
			if b.limit > 0 {
				canonical.limit = b.limit
			}
			canonical.scope = b.scope
			b.mu.Unlock()
			canonical.mu.Unlock()
			c.routes[l.provisional] = canonical
		} else {
			c.hashes[key] = b
			c.routes[l.provisional] = b
		}
	}
	if status == http.StatusTooManyRequests && scope == "global" && resetOK && reset.After(c.global) {
		c.global = reset
	}
	return nil
}
func parseNonnegativeInt(raw string) (int, bool, error) {
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, false, fmt.Errorf("malformed rate-limit integer")
	}
	return value, true, nil
}
func parseReset(headers http.Header, now time.Time) (time.Time, bool, error) {
	if raw := headers.Get("X-RateLimit-Reset-After"); raw != "" {
		seconds, err := strconv.ParseFloat(raw, 64)
		if err != nil || seconds < 0 {
			return time.Time{}, false, fmt.Errorf("malformed reset-after")
		}
		return now.Add(time.Duration(seconds * float64(time.Second))), true, nil
	}
	raw := headers.Get("X-RateLimit-Reset")
	if raw == "" {
		return time.Time{}, false, nil
	}
	epoch, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("malformed reset")
	}
	serverNow := now
	if date := headers.Get("Date"); date != "" {
		parsed, err := http.ParseTime(date)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("malformed date")
		}
		serverNow = parsed
	}
	delta := time.Unix(0, int64(epoch*float64(time.Second))).Sub(serverNow)
	if delta < 0 {
		delta = 0
	}
	return now.Add(delta), true, nil
}
func parseRetryAfter(raw string, now time.Time) (time.Time, bool, error) {
	if raw == "" {
		return time.Time{}, false, nil
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		if seconds < 0 {
			return time.Time{}, false, fmt.Errorf("negative retry-after")
		}
		return now.Add(time.Duration(seconds * float64(time.Second))), true, nil
	}
	parsed, err := http.ParseTime(raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("malformed retry-after")
	}
	if parsed.Before(now) {
		parsed = now
	}
	return parsed, true, nil
}
