package channelmonitor

import "strings"

// Error categories are matched in this order. group_access is omitted because
// this server has no sub2api-style group dimension; those messages fall through.
const (
	CategoryContentPolicy          = "content_policy"
	CategoryAuthentication         = "authentication"
	CategoryContextLimit           = "context_limit"
	CategoryInvalidRequest         = "invalid_request"
	CategoryModelUnsupported       = "model_unsupported"
	CategoryQuotaOrBalance         = "quota_or_balance"
	CategoryAccountPoolUnavailable = "account_pool_unavailable"
	CategoryRateOrCapacity         = "rate_or_capacity"
	CategoryTimeout                = "timeout"
	CategoryTransportOrStream      = "transport_or_stream"
	CategoryUpstreamForbidden      = "upstream_forbidden"
	CategoryNotFound               = "not_found"
	CategoryClientCancelled        = "client_cancelled"
	CategoryUpstream5xx            = "upstream_5xx"
	CategoryInternal               = "internal"
	CategoryOther                  = "other"
)

// Categories is the stable taxonomy order.
var Categories = []string{
	CategoryContentPolicy,
	CategoryAuthentication,
	CategoryContextLimit,
	CategoryInvalidRequest,
	CategoryModelUnsupported,
	CategoryQuotaOrBalance,
	CategoryAccountPoolUnavailable,
	CategoryRateOrCapacity,
	CategoryTimeout,
	CategoryTransportOrStream,
	CategoryUpstreamForbidden,
	CategoryNotFound,
	CategoryClientCancelled,
	CategoryUpstream5xx,
	CategoryInternal,
	CategoryOther,
}

// DefaultIgnoredCategories are excluded from error_rate and health scoring only.
var DefaultIgnoredCategories = []string{
	CategoryAuthentication,
	CategoryClientCancelled,
	CategoryContentPolicy,
	CategoryContextLimit,
	CategoryModelUnsupported,
	CategoryNotFound,
	CategoryQuotaOrBalance,
}

// ErrorInput is the upstream failure text available on a usage record.
type ErrorInput struct {
	StatusCode int
	Message    string
}

// Classify maps one upstream failure onto the taxonomy.
func Classify(input ErrorInput) string {
	text := strings.ToLower(strings.TrimSpace(input.Message))
	status := input.StatusCode

	if containsAny(text, "content policy", "content_policy", "safety policy", "moderation", "blocked keyword") {
		return CategoryContentPolicy
	}
	if status == 401 || containsAny(text, "unauthorized", "invalid api key", "invalid_api_key", "authentication", "api_key_disabled") {
		return CategoryAuthentication
	}
	if containsAny(text, "context window", "context length", "maximum prompt length", "too many tokens", "max_tokens") {
		return CategoryContextLimit
	}
	if containsAny(text, "failed to deserialize", "missing required parameter", "invalid request", "invalid_request", "tool_choice") {
		return CategoryInvalidRequest
	}
	if containsAny(text, "does not support the requested model", "not supported by any configured account", "model not supported", "unsupported model") {
		return CategoryModelUnsupported
	}
	if containsAny(text, "run out of credits", "insufficient balance", "insufficient quota", "subscription", "quota exceeded", "billing hard limit") {
		return CategoryQuotaOrBalance
	}
	if containsAny(text, "no available accounts", "no healthy account", "no healthy upstream account", "failover budget exhausted", "account pool") {
		return CategoryAccountPoolUnavailable
	}
	if status == 429 || containsAny(text, "rate limit", "rate_limit", "high demand", "overloaded", "concurrency limit", "capacity") {
		return CategoryRateOrCapacity
	}
	if status == 408 || status == 504 || containsAny(text, "timeout", "deadline exceeded", "error code: 524", "gateway time-out", "gateway timeout") {
		return CategoryTimeout
	}
	if containsAny(text, "transport", "stream_read_error", "connection reset", "connection refused", "tls", "http2", "missing terminal event", "unexpected eof") {
		return CategoryTransportOrStream
	}
	if status == 403 {
		return CategoryUpstreamForbidden
	}
	if status == 404 {
		return CategoryNotFound
	}
	if status == 499 || containsAny(text, "client cancelled", "client canceled", "context canceled") {
		return CategoryClientCancelled
	}
	if status >= 500 {
		return CategoryUpstream5xx
	}
	return CategoryOther
}

func knownCategory(value string) bool {
	for _, category := range Categories {
		if category == value {
			return true
		}
	}
	return false
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
