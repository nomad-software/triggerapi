package env

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"time"
)

// call is what the value of a variable is when it is a call of one of the
// built-in functions, and nothing else: a name, and what is in its brackets.
var call = regexp.MustCompile(`(?s)^\s*([a-z][a-z0-9_]*)\((.*)\)\s*$`)

// numbers is what is in the brackets of a function that takes whole
// numbers: nothing, one, or two separated by a comma.
var numbers = regexp.MustCompile(`^\s*(?:(-?\d+)\s*(?:,\s*(-?\d+)\s*)?)?$`)

// defaultRandomMax is the greatest number random_int() makes when it is not
// given one.
const defaultRandomMax = 1000

// now gives the time, which the tests replace.
var now = time.Now

// Evaluate returns what the value of a variable stands for if it is a call
// of a built-in function, which is worked out afresh each time, and reports
// if it was one. Anything else is a value as it stands. The text given to a
// function that takes text has its placeholders replaced by expand.
//
//   - random_uuid() is a random UUID.
//   - random_int() is a random whole number from 0 to 1000, random_int(n)
//     one from 0 to n, and random_int(m, n) one from m to n.
//   - iso_date() is the date today, as 2006-01-02, and iso_date(n) the date
//     n days from today, or before it if n is below zero.
//   - iso_timestamp() is the time now in UTC, as 2006-01-02T15:04:05Z, and
//     iso_timestamp(n) that time n days from now.
//   - unix_timestamp() is the time now as the number of seconds since the
//     start of 1970 in UTC, and unix_timestamp(n) that time n days from now.
//   - base64(text) is the text in base64, with padding.
//   - sha256(text) is the SHA-256 hash of the text, in lower case hex.
func Evaluate(value string, at time.Time, expand func(string) string) (string, bool) {
	parts := call.FindStringSubmatch(value)
	if parts == nil {
		return "", false
	}
	name, inside := parts[1], parts[2]

	// The text of a function that takes text is everything in its
	// brackets, as it is written.
	switch name {
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(expand(inside))), true
	case "sha256":
		sum := sha256.Sum256([]byte(expand(inside)))
		return hex.EncodeToString(sum[:]), true
	}

	// The others take no more than two whole numbers.
	given := numbers.FindStringSubmatch(inside)
	if given == nil {
		return "", false
	}
	var args []int64
	for _, arg := range given[1:] {
		if arg == "" {
			continue
		}
		n, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return "", false
		}
		args = append(args, n)
	}

	if name == "random_int" {
		low, high := int64(0), int64(defaultRandomMax)
		switch len(args) {
		case 1:
			high = args[0]
		case 2:
			low, high = args[0], args[1]
		}
		if high < low {
			return "", false
		}
		return randomInt(low, high), true
	}

	// The rest take one number of days, if any.
	if len(args) > 1 {
		return "", false
	}
	days := 0
	if len(args) == 1 {
		if int64(int(args[0])) != args[0] {
			return "", false
		}
		days = int(args[0])
	}
	switch name {
	case "random_uuid":
		if len(args) > 0 {
			return "", false
		}
		return randomUUID(), true
	case "iso_date":
		return at.AddDate(0, 0, days).Format(time.DateOnly), true
	case "iso_timestamp":
		return at.AddDate(0, 0, days).UTC().Format(time.RFC3339), true
	case "unix_timestamp":
		return strconv.FormatInt(at.AddDate(0, 0, days).Unix(), 10), true
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

// randomInt returns a random whole number from low to high, both included,
// each as likely as any other.
func randomInt(low, high int64) string {
	span := new(big.Int).Sub(big.NewInt(high), big.NewInt(low))
	span.Add(span, big.NewInt(1))
	// Reading random bytes does not fail.
	n, _ := rand.Int(rand.Reader, span)
	return n.Add(n, big.NewInt(low)).String()
}
