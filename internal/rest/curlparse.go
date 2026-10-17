package rest

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/nomad-software/triggerapi/internal/shell"
)

// IsCurl reports if text is a curl command: its first word is curl.
func IsCurl(text string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), "curl")
	return ok && (rest == "" || strings.ContainsAny(rest[:1], " \t\r\n\\"))
}

// curlValues are the options of curl, without their dashes, which take a
// value, so that the value is not taken for the address. Those which are
// not part of a request are skipped with their values.
var curlValues = map[string]bool{
	"X": true, "request": true, "H": true, "header": true, "url": true,
	"d": true, "data": true, "data-raw": true, "data-binary": true, "data-ascii": true, "data-urlencode": true, "json": true,
	"u": true, "user": true, "A": true, "user-agent": true, "e": true, "referer": true, "b": true, "cookie": true,
	"F": true, "form": true, "form-string": true, "T": true, "upload-file": true,
	"o": true, "output": true, "m": true, "max-time": true, "connect-timeout": true, "retry": true, "retry-delay": true, "retry-max-time": true,
	"w": true, "write-out": true, "x": true, "proxy": true, "U": true, "proxy-user": true, "cacert": true, "capath": true,
	"E": true, "cert": true, "key": true, "cert-type": true, "key-type": true, "pass": true, "c": true, "cookie-jar": true,
	"resolve": true, "connect-to": true, "K": true, "config": true, "limit-rate": true, "max-redirs": true, "r": true, "range": true,
	"interface": true, "D": true, "dump-header": true, "Y": true, "speed-limit": true, "y": true, "speed-time": true,
	"z": true, "time-cond": true, "C": true, "continue-at": true, "Q": true, "quote": true, "t": true, "telnet-option": true,
	"oauth2-bearer": true, "aws-sigv4": true, "ciphers": true, "local-port": true, "dns-servers": true, "unix-socket": true,
	"abstract-unix-socket": true, "trace": true, "trace-ascii": true, "stderr": true, "variable": true, "expand-url": true,
}

// ParseCurl reads a curl command, as Curl writes one or a browser copies
// one, into a request. What curl does that a request cannot, such as where
// the response is written, is left out, but a request that cannot be made
// as the command makes it, such as one which sends a file, is an error.
func ParseCurl(command string) (Request, error) {
	words, err := shell.Split(command)
	if err != nil {
		return Request{}, fmt.Errorf("the command cannot be read, as %s", err)
	}
	if len(words) == 0 || words[0] != "curl" {
		return Request{}, errors.New("it is not a curl command")
	}

	r := Request{Auth: Auth{Type: AuthNone}, BodyType: BodyNone}
	var (
		address, method string
		data            []string
		get, head, json bool
	)

	// option handles an option, which has the value given if it takes one.
	option := func(name, value string) error {
		switch name {
		case "X", "request":
			method = strings.ToUpper(value)
		case "url":
			if address == "" {
				address = value
			}
		case "H", "header":
			if strings.HasPrefix(value, "@") {
				return errors.New("its headers are read from a file")
			}
			if key, rest, ok := strings.Cut(value, ":"); ok {
				r.Headers = append(r.Headers, KeyValue{Key: strings.TrimSpace(key), Value: strings.TrimSpace(rest), Enabled: true})
			} else if key, ok := strings.CutSuffix(value, ";"); ok {
				// This is how curl is told to send a header with no value.
				r.Headers = append(r.Headers, KeyValue{Key: strings.TrimSpace(key), Enabled: true})
			}
		case "A", "user-agent":
			r.Headers = append(r.Headers, KeyValue{Key: "User-Agent", Value: value, Enabled: true})
		case "e", "referer":
			r.Headers = append(r.Headers, KeyValue{Key: "Referer", Value: value, Enabled: true})
		case "b", "cookie":
			// Without an = it is the name of a file to read them from.
			if strings.Contains(value, "=") {
				r.Headers = append(r.Headers, KeyValue{Key: "Cookie", Value: value, Enabled: true})
			}
		case "u", "user":
			username, password, _ := strings.Cut(value, ":")
			r.Auth = Auth{Type: AuthBasic, Username: username, Password: password}
		case "oauth2-bearer":
			r.Auth = Auth{Type: AuthBearer, Token: value}
		case "d", "data", "data-ascii", "data-binary", "json":
			if strings.HasPrefix(value, "@") {
				return errors.New("its body is read from a file")
			}
			if name != "data-binary" {
				// curl leaves out the line breaks of these.
				value = strings.NewReplacer("\r", "", "\n", "").Replace(value)
			}
			data = append(data, value)
			json = json || name == "json"
		case "data-raw":
			data = append(data, value)
		case "data-urlencode":
			encoded, err := urlEncoded(value)
			if err != nil {
				return err
			}
			data = append(data, encoded)
		case "G", "get":
			get = true
		case "I", "head":
			head = true
		case "F", "form", "form-string":
			return errors.New("it sends a multipart form, which a request cannot")
		case "T", "upload-file":
			return errors.New("it sends a file")
		}
		return nil
	}

	for i := 1; i < len(words); i++ {
		word := words[i]
		switch {
		case word == "--":
			// What follows is addresses.
			if address == "" && i+1 < len(words) {
				address = words[i+1]
			}
			i = len(words)
		case strings.HasPrefix(word, "--"):
			name := strings.TrimPrefix(word, "--")
			value := ""
			if curlValues[name] {
				if i+1 == len(words) {
					return Request{}, fmt.Errorf("%s is not given a value", word)
				}
				i++
				value = words[i]
			}
			if err := option(name, value); err != nil {
				return Request{}, err
			}
		case strings.HasPrefix(word, "-") && len(word) > 1:
			// Short options can be run together, and the value of the
			// last can follow it in the same word.
			letters := []rune(word[1:])
			for j, letter := range letters {
				name, value := string(letter), ""
				if curlValues[name] {
					if j+1 < len(letters) {
						value = string(letters[j+1:])
					} else if i+1 < len(words) {
						i++
						value = words[i]
					} else {
						return Request{}, fmt.Errorf("-%s is not given a value", name)
					}
				}
				if err := option(name, value); err != nil {
					return Request{}, err
				}
				if curlValues[name] {
					break
				}
			}
		case address == "":
			address = word
		}
	}
	if address == "" {
		return Request{}, errors.New("it has no address")
	}

	switch {
	case method != "":
	case head:
		method = http.MethodHead
	case len(data) > 0 && !get:
		method = http.MethodPost
	default:
		method = http.MethodGet
	}
	if !slices.Contains(Methods, method) {
		return Request{}, fmt.Errorf("its method, %s, is not one a request can have", method)
	}
	r.Method = method

	base, query, _ := strings.Cut(address, "?")
	r.URL, r.Params = base, queryParams(query)

	body := strings.Join(data, "&")
	switch {
	case len(data) == 0:
	case get:
		// The data is the query string.
		r.Params = append(r.Params, queryParams(body)...)
	default:
		r.setBody(body, json)
	}

	r.bearerAuth()
	return r, nil
}

// urlEncoded returns the value of a --data-urlencode option as it is sent:
// what follows the first = encoded, after the name before it, or all of it
// encoded if there is no =. An @ before any = names a file to read.
func urlEncoded(value string) (string, error) {
	i := strings.IndexAny(value, "=@")
	switch {
	case i < 0:
		return url.QueryEscape(value), nil
	case value[i] == '@':
		return "", errors.New("its body is read from a file")
	case i == 0:
		return url.QueryEscape(value[1:]), nil
	}
	return value[:i] + "=" + url.QueryEscape(value[i+1:]), nil
}

// setBody gives the request the body of the command, with the body type its
// content type says, and leaves out a Content-Type header which the body
// type gives in any case. Without one, curl sends a body as a form, and
// --json as JSON.
func (r *Request) setBody(body string, json bool) {
	contentType := "application/x-www-form-urlencoded"
	if json {
		contentType = "application/json"
		if !r.hasHeader("Accept") {
			r.Headers = append(r.Headers, KeyValue{Key: "Accept", Value: "application/json", Enabled: true})
		}
	}
	for _, h := range r.Headers {
		if strings.EqualFold(h.Key, "Content-Type") {
			contentType = h.Value
		}
	}

	media := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch {
	case media == "application/x-www-form-urlencoded":
		if form, ok := decodeForm(body); ok {
			r.BodyType, r.Form = BodyForm, form
		} else {
			r.BodyType, r.Body = BodyText, body
		}
	case media == "application/json" || strings.HasSuffix(media, "+json"):
		r.BodyType, r.Body = BodyJSON, body
	case media == "application/xml" || media == "text/xml" || strings.HasSuffix(media, "+xml"):
		r.BodyType, r.Body = BodyXML, body
	default:
		r.BodyType, r.Body = BodyText, body
	}

	// The header is kept unless it is the one the body type gives.
	if r.BodyType.ContentType() == contentType {
		r.dropHeaders(func(i int, h KeyValue) bool { return strings.EqualFold(h.Key, "Content-Type") })
	} else if !r.hasHeader("Content-Type") {
		r.Headers = append(r.Headers, KeyValue{Key: "Content-Type", Value: contentType, Enabled: true})
	}
}

// decodeForm returns the fields of a body encoded as a form, reporting
// false if it is not one.
func decodeForm(body string) ([]KeyValue, bool) {
	var fields []KeyValue
	for _, part := range strings.Split(body, "&") {
		if part == "" {
			continue
		}
		key, value, _ := strings.Cut(part, "=")
		key, err := url.QueryUnescape(key)
		if err != nil || key == "" {
			return nil, false
		}
		if value, err = url.QueryUnescape(value); err != nil {
			return nil, false
		}
		fields = append(fields, KeyValue{Key: key, Value: value, Enabled: true})
	}
	return fields, true
}

// queryParams returns the params of the query string of an address, which
// is encoded as a form is. Where it is not validly encoded, they are read as
// the URL field reads them.
func queryParams(query string) []KeyValue {
	if params, ok := decodeForm(query); ok {
		return params
	}
	_, params := SplitURL("?" + query)
	return params
}

// hasHeader reports if the request has a header of the name.
func (r *Request) hasHeader(name string) bool {
	return slices.ContainsFunc(r.Headers, func(h KeyValue) bool { return strings.EqualFold(h.Key, name) })
}

// bearerAuth takes the token of a lone Authorization header of the bearer
// scheme into the authorization of the request, which is where it is put in
// the request UI.
func (r *Request) bearerAuth() {
	if r.Auth.Type != AuthNone {
		return
	}
	var found []int
	for i, h := range r.Headers {
		if strings.EqualFold(h.Key, "Authorization") {
			found = append(found, i)
		}
	}
	if len(found) != 1 {
		return
	}
	scheme, token, ok := strings.Cut(r.Headers[found[0]].Value, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return
	}
	r.Auth = Auth{Type: AuthBearer, Token: strings.TrimSpace(token)}
	r.dropHeaders(func(i int, _ KeyValue) bool { return i == found[0] })
}

// dropHeaders takes out the headers that drop reports true for, leaving the
// request with none rather than an empty list if it takes them all.
func (r *Request) dropHeaders(drop func(int, KeyValue) bool) {
	var kept []KeyValue
	for i, h := range r.Headers {
		if !drop(i, h) {
			kept = append(kept, h)
		}
	}
	r.Headers = kept
}
