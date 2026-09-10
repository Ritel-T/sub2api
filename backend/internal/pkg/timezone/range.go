package timezone

import "time"

// ParseRangeBoundary preserves timestamp precision and treats date-only ends
// as the next local midnight for half-open database ranges.
func ParseRangeBoundary(value, userTZ string, end bool) (time.Time, error) {
	if len(value) != len("2006-01-02") {
		return time.Parse(time.RFC3339Nano, value)
	}
	t, err := ParseInUserLocation("2006-01-02", value, userTZ)
	if err == nil && end {
		t = t.AddDate(0, 0, 1)
	}
	return t, err
}
