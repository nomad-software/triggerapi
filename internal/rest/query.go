package rest

import (
	"net/url"
	"strings"
)

// These helpers keep the query string shown in the URL field and the params
// table in step. The query is shown as typed rather than fully encoded, so
// placeholders and most values stay readable; only the characters that would
// change its structure are escaped.

var (
	valueEscaper = strings.NewReplacer("%", "%25", "&", "%26", "#", "%23")
	keyEscaper   = strings.NewReplacer("%", "%25", "&", "%26", "#", "%23", "=", "%3D")
)

// JoinURL appends the enabled params to base as a query string for display.
func JoinURL(base string, params []KeyValue) string {
	var parts []string
	for _, p := range params {
		if !p.Enabled || p.Key == "" {
			continue
		}
		parts = append(parts, keyEscaper.Replace(p.Key)+"="+valueEscaper.Replace(p.Value))
	}
	if len(parts) == 0 {
		return base
	}
	return base + "?" + strings.Join(parts, "&")
}

// SplitURL separates a URL as typed into the part before the query string
// and the params in it. It is the inverse of JoinURL.
func SplitURL(raw string) (base string, params []KeyValue) {
	base, query, _ := strings.Cut(raw, "?")

	for _, part := range strings.Split(query, "&") {
		key, value, _ := strings.Cut(part, "=")
		key, value = unescape(key), unescape(value)
		if key == "" {
			continue
		}
		params = append(params, KeyValue{Key: key, Value: value, Enabled: true})
	}

	return base, params
}

// unescape decodes percent escapes, leaving text that is not validly escaped
// as it is.
func unescape(s string) string {
	if decoded, err := url.PathUnescape(s); err == nil {
		return decoded
	}
	return s
}

// MergeParams returns existing with its enabled params replaced, in order, by
// those typed into the URL. Disabled params are not part of the URL and keep
// their place.
func MergeParams(existing, typed []KeyValue) []KeyValue {
	var merged []KeyValue
	for _, p := range existing {
		switch {
		case !p.Enabled:
			merged = append(merged, p)
		case len(typed) > 0:
			merged = append(merged, typed[0])
			typed = typed[1:]
		}
	}
	return append(merged, typed...)
}
