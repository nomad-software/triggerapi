package rest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Curl returns a curl command which makes the request, to be run in a POSIX
// shell. It is made from the request as built, so it sends the address,
// headers and body that Send would. Placeholders should have been expanded
// first.
func (r Request) Curl() (string, error) {
	options, err := r.curlOptions()
	if err != nil {
		return "", err
	}

	// The first line is the command with the options that say how the
	// request is made, and its address. Each option after that has a line.
	lines := []string{"curl"}
	for i, option := range options {
		words := make([]string, len(option))
		for j, word := range option {
			words[j] = shellQuote(word)
		}
		if i == 0 {
			lines[0] += " " + strings.Join(words, " ")
		} else {
			lines = append(lines, strings.Join(words, " "))
		}
	}
	return strings.Join(lines, " \\\n  "), nil
}

// curlOptions returns the arguments of the curl command which makes the
// request, an option and its value together. The first holds the options
// which say how the request is made, followed by its address.
func (r Request) curlOptions() ([][]string, error) {
	req, err := r.Build(context.Background())
	if err != nil {
		return nil, err
	}
	address := req.URL.String()
	content := r.content()

	// Send follows redirects, so the command does.
	first := []string{"--location"}
	switch {
	case req.Method == http.MethodGet && content == "":
		// This is what curl does when it is not told otherwise.
	case req.Method == http.MethodHead && content == "":
		first = append(first, "--head")
	default:
		first = append(first, "--request", req.Method)
	}
	// Braces and brackets in an address would be taken by curl for a range
	// of addresses. They are usually escaped by now, but not always.
	if strings.ContainsAny(address, "{}[]") {
		first = append(first, "--globoff")
	}
	options := [][]string{append(first, address)}

	// Basic authorization is given as the name and password where it can
	// be, which is easier to read than the header made from them.
	if value, ok := r.Auth.Header(); ok && r.Auth.Type == AuthBasic && !strings.Contains(r.Auth.Username, ":") && slices.Equal(req.Header.Values("Authorization"), []string{value}) {
		req.Header.Del("Authorization")
		options = append(options, []string{"--user", r.Auth.Username + ":" + r.Auth.Password})
	}

	if req.Host != "" && req.Host != req.URL.Host {
		options = append(options, []string{"--header", "Host: " + req.Host})
	}
	names := make([]string, 0, len(req.Header))
	for name := range req.Header {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, value := range req.Header[name] {
			header := name + ": " + value
			if value == "" {
				// This is how curl is told to send a header with no value.
				header = name + ";"
			}
			options = append(options, []string{"--header", header})
		}
	}

	if content != "" {
		// Unlike --data, this does not read a file when the body starts
		// with an @.
		options = append(options, []string{"--data-raw", content})
	}

	return options, nil
}

// RunCurl makes the request by running the curl command that Curl describes,
// and returns the response it was given. The curl program must be installed.
func RunCurl(ctx context.Context, r Request) (*Response, error) {
	options, err := r.curlOptions()
	if err != nil {
		return nil, err
	}
	program, err := exec.LookPath("curl")
	if err != nil {
		return nil, errors.New("curl is not installed, or is not on the PATH")
	}

	// The headers are written to a file, which keeps them apart from the
	// body however the response is made up.
	file, err := os.CreateTemp("", "triggerapi-headers-*")
	if err != nil {
		return nil, err
	}
	file.Close()
	defer os.Remove(file.Name())

	// The progress meter is left out, but not the reason for a failure.
	args := []string{"--silent", "--show-error", "--dump-header", file.Name()}
	head := false
	for _, option := range options {
		head = head || slices.Contains(option, "--head")
		args = append(args, option...)
	}

	var body, problem bytes.Buffer
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Stdout = &body
	cmd.Stderr = &problem

	start := time.Now()
	err = cmd.Run()
	duration := time.Since(start)

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if message := strings.TrimSpace(problem.String()); message != "" {
			return nil, errors.New(message)
		}
		return nil, fmt.Errorf("curl: %w", err)
	}

	headers, err := os.ReadFile(file.Name())
	if err != nil {
		return nil, err
	}
	res, err := parseCurlHeaders(string(headers))
	if err != nil {
		return nil, err
	}
	// When only the headers are asked for, curl prints them as if they were
	// the body.
	if !head {
		res.Body = body.Bytes()
	}
	res.Duration = duration
	return res, nil
}

// parseCurlHeaders reads the status and headers of a response from what curl
// wrote of them. There is a set for each response on the way to the last,
// such as a redirect, and it is the last that is read.
func parseCurlHeaders(dump string) (*Response, error) {
	var last []string
	for _, line := range strings.Split(strings.ReplaceAll(dump, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "HTTP/") {
			last = nil
		}
		if line != "" {
			last = append(last, line)
		}
	}
	if len(last) == 0 || !strings.HasPrefix(last[0], "HTTP/") {
		return nil, errors.New("curl did not report a response")
	}

	proto, status, _ := strings.Cut(last[0], " ")
	status = strings.TrimSpace(status)
	code, text, _ := strings.Cut(status, " ")
	res := &Response{Status: status, Proto: proto, Headers: http.Header{}}
	res.StatusCode, _ = strconv.Atoi(code)
	// Only HTTP/1 has the reason after the code.
	if text == "" && http.StatusText(res.StatusCode) != "" {
		res.Status = code + " " + http.StatusText(res.StatusCode)
	}
	// Named as Send names them.
	if !strings.Contains(proto, ".") {
		res.Proto = proto + ".0"
	}

	for _, line := range last[1:] {
		if name, value, ok := strings.Cut(line, ":"); ok {
			res.Headers.Add(strings.TrimSpace(name), strings.TrimSpace(value))
		}
	}
	return res, nil
}

// shellQuote returns s as a single word for a POSIX shell.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:=@%+,", r))
	}) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
