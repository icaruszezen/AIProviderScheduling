package auth

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// channelConnectionReleases tracks connection leases acquired while one execution
// walks candidate channels. releaseAll is idempotent for each lease.
type channelConnectionReleases struct {
	pending []func()
}

func (r *channelConnectionReleases) add(release func()) {
	if r == nil || release == nil {
		return
	}
	r.pending = append(r.pending, release)
}

func (r *channelConnectionReleases) releaseAll() {
	if r == nil || len(r.pending) == 0 {
		return
	}
	pending := r.pending
	r.pending = nil
	for i := len(pending) - 1; i >= 0; i-- {
		if pending[i] != nil {
			pending[i]()
		}
	}
}

func (r *channelConnectionReleases) detachLatest() func() {
	if r == nil || len(r.pending) == 0 {
		return func() {}
	}
	last := len(r.pending) - 1
	release := r.pending[last]
	r.pending = r.pending[:last]
	if release == nil {
		return func() {}
	}
	return release
}

// MaxConcurrentConnections reports the channel cap stored on the auth.
// Zero means the channel is unlimited.
func (a *Auth) MaxConcurrentConnections() int {
	if a == nil {
		return 0
	}
	return maxConcurrentConnectionsFromMetadata(a.Metadata)
}

const channelConnectionLimitMessage = "channel connection limit reached"

func maxConcurrentConnectionsFromMetadata(metadata map[string]any) int {
	if len(metadata) == 0 {
		return 0
	}
	for _, key := range []string{"max_concurrent_connections", "max-concurrent-connections"} {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		return positiveMetadataInt(value)
	}
	return 0
}

func channelConnectionLimitReachedError() error {
	return &Error{Code: "auth_not_found", Message: channelConnectionLimitMessage}
}

// preferChannelConnectionLimitError reports a full channel when selection found
// nobody and this round never reached an upstream attempt. HTTPStatus stays 0 so
// the outer retry loop does not spin; the handler later presents 503.
func preferChannelConnectionLimitError(blocked bool, lastErr, fallback error) error {
	if blocked && lastErr == nil {
		return channelConnectionLimitReachedError()
	}
	return fallback
}

func positiveMetadataInt(raw any) int {
	switch value := raw.(type) {
	case int:
		if value > 0 {
			return value
		}
	case int32:
		if value > 0 {
			return int(value)
		}
	case int64:
		if value > 0 {
			return int(value)
		}
	case float64:
		if value > 0 {
			return int(value)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func (m *Manager) channelConnectionCounter(authID string) *atomic.Int64 {
	authID = strings.TrimSpace(authID)
	if m == nil || authID == "" {
		return &atomic.Int64{}
	}
	if existing, ok := m.channelConnections.Load(authID); ok {
		if counter, ok := existing.(*atomic.Int64); ok && counter != nil {
			return counter
		}
	}
	counter := &atomic.Int64{}
	actual, _ := m.channelConnections.LoadOrStore(authID, counter)
	if stored, ok := actual.(*atomic.Int64); ok && stored != nil {
		return stored
	}
	return counter
}

// ActiveChannelConnections reports how many requests currently occupy the channel.
func (m *Manager) ActiveChannelConnections(authID string) int64 {
	if m == nil {
		return 0
	}
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return 0
	}
	existing, ok := m.channelConnections.Load(authID)
	if !ok {
		return 0
	}
	counter, ok := existing.(*atomic.Int64)
	if !ok || counter == nil {
		return 0
	}
	return counter.Load()
}

// tryAcquireChannelConnection reserves one in-flight slot on the live channel.
// A nil release with ok false means the channel is at its cap. Unlimited channels
// still count so the management UI can show current occupancy.
func (m *Manager) tryAcquireChannelConnection(auth *Auth) (func(), bool) {
	if m == nil || auth == nil {
		return func() {}, true
	}
	id := strings.TrimSpace(auth.ID)
	if id == "" {
		return func() {}, true
	}
	limitAuth := auth
	m.mu.RLock()
	if current := m.auths[id]; current != nil {
		limitAuth = current
	}
	m.mu.RUnlock()
	limit := limitAuth.MaxConcurrentConnections()
	counter := m.channelConnectionCounter(id)
	if !tryIncrementConnection(counter, limit) {
		return nil, false
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			for {
				current := counter.Load()
				if current <= 0 {
					return
				}
				if counter.CompareAndSwap(current, current-1) {
					return
				}
			}
		})
	}, true
}

func tryIncrementConnection(counter *atomic.Int64, limit int) bool {
	if counter == nil {
		return true
	}
	for {
		current := counter.Load()
		if limit > 0 && current >= int64(limit) {
			return false
		}
		if counter.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

// holdChannelConnection occupies a local channel. Home selection does not use this cap.
// False means the channel is full and the caller should try the next credential.
func (m *Manager) holdChannelConnection(homeMode bool, auth *Auth, releases *channelConnectionReleases) bool {
	if homeMode {
		return true
	}
	release, ok := m.tryAcquireChannelConnection(auth)
	if !ok {
		return false
	}
	if releases != nil {
		releases.add(release)
	}
	return true
}

func wrapChannelConnectionStream(ctx context.Context, result *cliproxyexecutor.StreamResult, release func()) *cliproxyexecutor.StreamResult {
	if release == nil {
		release = func() {}
	}
	if result == nil || result.Chunks == nil {
		release()
		return result
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out := make(chan cliproxyexecutor.StreamChunk)
	go func() {
		defer close(out)
		released := false
		releaseOnce := func() {
			if released {
				return
			}
			released = true
			release()
		}
		defer releaseOnce()
		// Release the concurrency permit before discarding the rest of the upstream
		// stream. A cancelled request must not keep the channel slot until the producer closes.
		drain := func() {
			releaseOnce()
			go func() {
				for range result.Chunks {
				}
			}()
		}
		for {
			select {
			case <-ctx.Done():
				drain()
				return
			case chunk, ok := <-result.Chunks:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					drain()
					return
				case out <- chunk:
				}
			}
		}
	}()
	return &cliproxyexecutor.StreamResult{Headers: result.Headers, Chunks: out}
}
