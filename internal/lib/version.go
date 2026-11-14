package lib

import "strconv"

// IsLatest reports whether version a is later than version b. Versions are integer strings, the
// only shape the data schema allows; a version that is not one is never the latest.
func IsLatest(a, b string) bool {
	x, err := strconv.Atoi(a)
	if err != nil {
		return false
	}
	y, err := strconv.Atoi(b)
	if err != nil {
		return true
	}
	return x > y
}
