package env

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// call is what the value of a variable is when it is a call of one of the
// built-in functions, and nothing else: a name, and in brackets nothing or a
// whole number.
var call = regexp.MustCompile(`^\s*([a-z_]+)\(\s*(-?\d+)?\s*\)\s*$`)

// now gives the time, which the tests replace.
var now = time.Now

// Evaluate returns what the value of a variable stands for if it is a call
// of a built-in function, which is worked out afresh each time, and reports
// if it was one. Anything else is a value as it stands.
//
//   - random_uuid() is a random UUID.
//   - iso_date() is the date today, as 2006-01-02, and iso_date(n) the date
//     n days from today, or before it if n is below zero.
//   - iso_timestamp() is the time now in UTC, as 2006-01-02T15:04:05Z, and
//     iso_timestamp(n) that time n days from now.
func Evaluate(value string, at time.Time) (string, bool) {
	parts := call.FindStringSubmatch(value)
	if parts == nil {
		return "", false
	}

	days, given := 0, parts[2] != ""
	if given {
		var err error
		if days, err = strconv.Atoi(parts[2]); err != nil {
			return "", false
		}
	}

	switch parts[1] {
	case "random_uuid":
		if given {
			return "", false
		}
		return randomUUID(), true
	case "iso_date":
		return at.AddDate(0, 0, days).Format(time.DateOnly), true
	case "iso_timestamp":
		return at.AddDate(0, 0, days).UTC().Format(time.RFC3339), true
	}
	return "", false
}

// randomUUID returns a random (version 4) UUID.
func randomUUID() string {
	var b [16]byte
	// Reading random bytes does not fail.
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
