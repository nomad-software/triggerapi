// Package oauthtest is an OAuth 2.0 authorization server for tests, with the
// client credentials, authorization code (PKCE), device code and refresh
// token grants, and a resource that wants one of its tokens.
package oauthtest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The client the server knows.
const (
	ClientID     = "client"
	ClientSecret = "secret"
)

// Server is the authorization server. Its paths are /token, /authorize (which
// signs the user in at once and sends the browser back), /device, /verify
// (which approves a device code, by its user code) and /resource, which
// answers 200 to a request with a valid token and 401 to any other.
type Server struct {
	*httptest.Server

	mu sync.Mutex
	// ExpiresIn is how long a token lasts, which is an hour unless it is
	// set. A negative value gives tokens that have already expired.
	ExpiresIn time.Duration
	// Deny has /authorize send the browser back with access_denied.
	Deny bool
	// issued counts the tokens given, valid the access tokens that can be
	// used, refresh the refresh tokens, and codes the authorization codes
	// with the challenge and redirect URL each was given for.
	issued  int
	valid   map[string]bool
	refresh map[string]bool
	codes   map[string]grant
	devices map[string]*device
	// requests are the forms of the requests to /token, in order.
	requests []url.Values
}

type grant struct {
	challenge, redirect string
}

type device struct {
	user     string
	approved bool
}

// New starts a server, which is closed when the test ends.
func New(t interface{ Cleanup(func()) }) *Server {
	s := &Server{valid: map[string]bool{}, refresh: map[string]bool{}, codes: map[string]grant{}, devices: map[string]*device{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", s.token)
	mux.HandleFunc("/authorize", s.authorize)
	mux.HandleFunc("/device", s.device)
	mux.HandleFunc("/verify", s.verify)
	mux.HandleFunc("/resource", s.resource)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// Issued returns how many tokens have been given.
func (s *Server) Issued() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issued
}

// Requests returns the forms of the requests made to /token.
func (s *Server) Requests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.requests...)
}

// Valid reports if an access token was given by the server.
func (s *Server) Valid(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.valid[token]
}

// Revoke makes every token given so far unusable, refresh tokens too.
func (s *Server) Revoke() {
	s.mu.Lock()
	clear(s.valid)
	clear(s.refresh)
	s.mu.Unlock()
}

func (s *Server) resource(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !s.Valid(token) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	back, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || q.Get("client_id") != ClientID || q.Get("response_type") != "code" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	answer := url.Values{"state": {q.Get("state")}}
	s.mu.Lock()
	if s.Deny {
		answer.Set("error", "access_denied")
		answer.Set("error_description", "The user said no")
	} else if q.Get("code_challenge_method") == "S256" {
		code := fmt.Sprintf("code-%d", len(s.codes)+1)
		s.codes[code] = grant{q.Get("code_challenge"), q.Get("redirect_uri")}
		answer.Set("code", code)
	} else {
		answer.Set("error", "invalid_request")
		answer.Set("error_description", "PKCE is required")
	}
	s.mu.Unlock()
	back.RawQuery = answer.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (s *Server) device(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if r.Form.Get("client_id") != ClientID {
		fail(w, "invalid_client", "")
		return
	}
	s.mu.Lock()
	code := fmt.Sprintf("device-%d", len(s.devices)+1)
	user := fmt.Sprintf("USER-%d", len(s.devices)+1)
	s.devices[code] = &device{user: user}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"device_code":               code,
		"user_code":                 user,
		"verification_uri":          s.URL + "/verify",
		"verification_uri_complete": s.URL + "/verify?user_code=" + user,
		"expires_in":                60,
		"interval":                  1,
	})
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	user := r.URL.Query().Get("user_code")
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.devices {
		if d.user == user {
			d.approved = true
			fmt.Fprint(w, "approved")
			return
		}
	}
	http.Error(w, "unknown code", http.StatusNotFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.PostForm)

	id, secret, basic := r.BasicAuth()
	if !basic {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}

	refresh := false
	switch r.PostForm.Get("grant_type") {
	case "client_credentials":
		if id != ClientID || secret != ClientSecret {
			s.unauthorised(w, basic)
			return
		}
	case "authorization_code":
		code := r.PostForm.Get("code")
		g, ok := s.codes[code]
		delete(s.codes, code)
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		switch {
		case id != ClientID:
			s.unauthorised(w, basic)
			return
		case !ok:
			fail(w, "invalid_grant", "unknown code")
			return
		case base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge:
			fail(w, "invalid_grant", "the code verifier does not match")
			return
		case r.PostForm.Get("redirect_uri") != g.redirect:
			fail(w, "invalid_grant", "the redirect URI does not match")
			return
		}
		refresh = true
	case "urn:ietf:params:oauth:grant-type:device_code":
		d, ok := s.devices[r.PostForm.Get("device_code")]
		switch {
		case !ok:
			fail(w, "invalid_grant", "")
			return
		case !d.approved:
			fail(w, "authorization_pending", "")
			return
		}
		delete(s.devices, r.PostForm.Get("device_code"))
		refresh = true
	case "refresh_token":
		token := r.PostForm.Get("refresh_token")
		if !s.refresh[token] {
			fail(w, "invalid_grant", "the refresh token is not valid")
			return
		}
		delete(s.refresh, token)
		refresh = true
	default:
		fail(w, "unsupported_grant_type", "")
		return
	}

	s.issued++
	access := fmt.Sprintf("token-%d", s.issued)
	s.valid[access] = true
	expires := s.ExpiresIn
	if expires == 0 {
		expires = time.Hour
	}
	answer := map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(expires / time.Second),
	}
	if scope := r.PostForm.Get("scope"); scope != "" {
		answer["scope"] = scope
	}
	if refresh {
		token := fmt.Sprintf("refresh-%d", s.issued)
		s.refresh[token] = true
		answer["refresh_token"] = token
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(answer)
}

// unauthorised refuses a client that is not known, which is the status 401
// when it gave its secret in the header.
func (s *Server) unauthorised(w http.ResponseWriter, basic bool) {
	w.Header().Set("Content-Type", "application/json")
	if basic {
		w.WriteHeader(http.StatusUnauthorized)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}
	json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client", "error_description": "unknown client"})
}

func fail(w http.ResponseWriter, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	answer := map[string]string{"error": code}
	if description != "" {
		answer["error_description"] = description
	}
	json.NewEncoder(w).Encode(answer)
}
