package config

import "sort"

const (
	// MaxProviderRetryCount is the upper bound for same-credential retries.
	MaxProviderRetryCount = 10
)

// DefaultProviderRetryStatusCodes is used when provider-retry-count is positive
// and provider-retry-status-codes is omitted.
var DefaultProviderRetryStatusCodes = []int{401, 403, 429}

// SanitizeProviderRetryCount treats negative values as unset and clamps values above 10.
func SanitizeProviderRetryCount(count *int) *int {
	if count == nil {
		return nil
	}
	if *count < 0 {
		return nil
	}
	if *count > MaxProviderRetryCount {
		clamped := MaxProviderRetryCount
		return &clamped
	}
	return count
}

// SanitizeProviderRetryStatusCodes keeps a nil pointer as omitted, filters
// codes to HTTP 100-599, and deduplicates them in ascending order. An explicit
// empty list is preserved so callers can disable status-code retries.
func SanitizeProviderRetryStatusCodes(codes *[]int) *[]int {
	if codes == nil {
		return nil
	}
	seen := make(map[int]struct{}, len(*codes))
	out := make([]int, 0, len(*codes))
	for _, code := range *codes {
		if code < 100 || code > 599 {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	sort.Ints(out)
	return &out
}

func sanitizeProviderRetryFields(count **int, codes **[]int) {
	if count != nil {
		*count = SanitizeProviderRetryCount(*count)
	}
	if codes != nil {
		*codes = SanitizeProviderRetryStatusCodes(*codes)
	}
}
