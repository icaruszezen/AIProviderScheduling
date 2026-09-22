package channelmonitor

import "math"

const histBuckets = 17

// histBounds are inclusive upper bounds in milliseconds. The last bucket is open-ended.
var histBounds = [histBuckets]int64{
	50, 100, 250, 500, 1000, 2000, 3000, 5000, 8000, 10000,
	15000, 30000, 60000, 120000, 300000, 600000, math.MaxInt64,
}

func histogramIndex(ms int64) int {
	if ms < 0 {
		ms = 0
	}
	for i, bound := range histBounds {
		if ms <= bound {
			return i
		}
	}
	return histBuckets - 1
}

func percentile(hist [histBuckets]int64, p float64) *int64 {
	var total int64
	for _, count := range hist {
		total += count
	}
	if total <= 0 || p <= 0 {
		return nil
	}
	target := p * float64(total)
	var cumulative int64
	var lower int64
	for i, count := range hist {
		upper := histBounds[i]
		if count <= 0 {
			if upper != math.MaxInt64 {
				lower = upper
			}
			continue
		}
		if float64(cumulative+count) >= target {
			spanUpper := upper
			if spanUpper == math.MaxInt64 {
				spanUpper = lower
			}
			if spanUpper < lower {
				spanUpper = lower
			}
			fraction := (target - float64(cumulative)) / float64(count)
			value := int64(math.Round(float64(lower) + fraction*float64(spanUpper-lower)))
			return &value
		}
		cumulative += count
		if upper != math.MaxInt64 {
			lower = upper
		}
	}
	return nil
}
