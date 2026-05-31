package util

// FirstNonEmpty returns the first non-empty string among values, or "" if all
// are empty.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
