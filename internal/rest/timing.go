package rest

import (
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"
)

// Stage is a part of the time a request takes.
type Stage string

const (
	// StageRedirects is the time taken by the responses that redirected to
	// another address, before the request that was answered was made.
	StageRedirects Stage = "redirects"
	StageDNS       Stage = "dns"
	StageConnect   Stage = "connect"
	StageTLS       Stage = "tls"
	// StageSend is the writing of the request, its headers and its body.
	StageSend Stage = "send"
	// StageWait is from the request having been sent to the first byte of
	// the response, which is mostly the time the server took.
	StageWait     Stage = "wait"
	StageDownload Stage = "download"
)

// Stages lists the stages in the order they come in.
var Stages = []Stage{StageRedirects, StageDNS, StageConnect, StageTLS, StageSend, StageWait, StageDownload}

// Phase is when a stage of a request began and ended, measured from when the
// request was started.
type Phase struct {
	Stage Stage         `json:"stage"`
	Start time.Duration `json:"start"`
	End   time.Duration `json:"end"`
}

// Duration is how long the phase took.
func (p Phase) Duration() time.Duration { return p.End - p.Start }

// Timing says where the time a request took went. It is kept in the history
// with the response.
type Timing struct {
	// Phases are those of the stages there were, in the order of Stages.
	// A stage there was no need for, such as looking up an address that is
	// an IP address, or connecting on a connection that was reused, has
	// none.
	Phases []Phase `json:"phases,omitempty"`
	// Redirects is how many responses redirected to another address.
	Redirects int `json:"redirects,omitempty"`
	// Reused records that the request went on a connection kept open from
	// one before, so there was no looking up, connecting or handshake.
	Reused bool `json:"reused,omitempty"`
	// Address is the IP address and port of the server.
	Address string `json:"address,omitempty"`
	// TLS is the version of TLS and the cipher suite, if there was TLS.
	TLS string `json:"tls,omitempty"`
}

// Phase returns the phase of a stage, and reports false if there was none.
func (t *Timing) Phase(stage Stage) (Phase, bool) {
	for _, p := range t.Phases {
		if p.Stage == stage {
			return p, true
		}
	}
	return Phase{}, false
}

// tracer records the times of the events of a request, through the hooks of
// httptrace, and makes a Timing of them. The hooks of a connection being
// made can be called on goroutines of their own, so it is locked. Only the
// last request is recorded where there are redirects; the time before it was
// made is that of the redirects.
type tracer struct {
	mu    sync.Mutex
	start time.Time
	// hops is how many requests have been made: one more than there were
	// redirects.
	hops int
	// hop is when the request being made was started, and events what
	// happened as it was made.
	hop time.Time
	events
}

// events are when each event of a request happened, and what came of them.
type events struct {
	dnsStart, dnsDone, connectStart, connectDone, tlsStart, tlsDone time.Time
	gotConn, wrote, firstByte                                       time.Time
	reused                                                          bool
	address                                                         string
}

func newTracer(start time.Time) *tracer {
	return &tracer{start: start}
}

// note records the time of an event under the lock, unless it has been
// recorded for the request being made.
func (t *tracer) note(at *time.Time) {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if at.IsZero() {
		*at = now
	}
}

// trace returns the hooks that record the events.
func (t *tracer) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		// A connection is asked for once for every request, including each
		// one made after a redirect, which starts the record again.
		GetConn: func(string) {
			now := time.Now()
			t.mu.Lock()
			defer t.mu.Unlock()
			t.hops++
			t.hop = now
			t.events = events{}
		},
		DNSStart: func(httptrace.DNSStartInfo) { t.note(&t.dnsStart) },
		DNSDone:  func(httptrace.DNSDoneInfo) { t.note(&t.dnsDone) },
		// Several addresses can be tried at once, so it is the first try
		// that starts connecting and the first to succeed that ends it.
		ConnectStart: func(string, string) { t.note(&t.connectStart) },
		ConnectDone: func(_, _ string, err error) {
			if err == nil {
				t.note(&t.connectDone)
			}
		},
		TLSHandshakeStart: func() { t.note(&t.tlsStart) },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				t.note(&t.tlsDone)
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			now := time.Now()
			t.mu.Lock()
			defer t.mu.Unlock()
			t.gotConn = now
			t.reused = info.Reused
			if info.Conn != nil {
				t.address = info.Conn.RemoteAddr().String()
			}
		},
		WroteRequest:         func(httptrace.WroteRequestInfo) { t.note(&t.wrote) },
		GotFirstResponseByte: func() { t.note(&t.firstByte) },
	}
}

// timing returns the Timing of the request, which ended with the last of its
// response being read at end, and was answered with res.
func (t *tracer) timing(end time.Time, res *http.Response) *Timing {
	t.mu.Lock()
	defer t.mu.Unlock()

	timing := &Timing{Reused: t.reused, Address: t.address}
	if t.hops > 1 {
		timing.Redirects = t.hops - 1
	}
	add := func(stage Stage, from, to time.Time) {
		if from.IsZero() || to.IsZero() || to.Before(from) {
			return
		}
		timing.Phases = append(timing.Phases, Phase{stage, from.Sub(t.start), to.Sub(t.start)})
	}

	if timing.Redirects > 0 {
		add(StageRedirects, t.start, t.hop)
	}
	// A connection that is reused was given while one was still being made,
	// whose events are not those of the request.
	if !t.reused {
		add(StageDNS, t.dnsStart, t.dnsDone)
		add(StageConnect, t.connectStart, t.connectDone)
		add(StageTLS, t.tlsStart, t.tlsDone)
	}
	add(StageSend, t.gotConn, t.wrote)
	add(StageWait, t.wrote, t.firstByte)
	add(StageDownload, t.firstByte, end)

	if res != nil && res.TLS != nil {
		timing.TLS = tls.VersionName(res.TLS.Version) + ", " + tls.CipherSuiteName(res.TLS.CipherSuite)
	}
	return timing
}
