package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nomad-software/triggerapi/internal/rest"
)

// Client talks to one server, for the calls and the discovery that share it.
//
// Since protocol version 2026-07-28 MCP is stateless (SEP-2575): sessions
// are deprecated, and a client asks the server what it has with
// server/discover and gives its protocol version, information and
// capabilities with every request. A server from before then needs the
// initialize handshake, and gives a session (the Mcp-Session-Id header)
// which every request has to carry. The SDK tries the first and falls back
// to the second, and Client keeps what it connected with, which for an older
// server is its session: connecting again if that server ends it.
//
// It connects the first time it is used, and is safe to use from more than
// one goroutine.
type Client struct {
	client  *http.Client
	request Request
	// version is that of the program, which the server is told.
	version string

	mu   sync.Mutex
	conn *sdk.ClientSession
}

// NewClient returns a client of the server of a request, which has had its
// placeholders replaced. It does not connect until it is used. The HTTP
// requests are made with client, with the headers and authorization of the
// request.
func NewClient(client *http.Client, r Request, version string) *Client {
	return &Client{client: client, request: r, version: version}
}

// connected returns the connection to the server, connecting first if
// there is none.
func (c *Client) connected(ctx context.Context) (*sdk.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn, nil
	}

	endpoint, err := c.request.endpoint()
	if err != nil {
		return nil, err
	}

	client := sdk.NewClient(&sdk.Implementation{Name: "TriggerAPI", Version: c.version}, nil)
	httpClient := *c.client
	httpClient.Transport = &headers{base: c.client.Transport, request: c.request}
	transport := &sdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &httpClient,
		// Nothing the server sends of its own accord is wanted, only the
		// answers to calls, so no stream is kept open for it.
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}
	conn, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	c.conn = conn
	return conn, nil
}

// forget drops a connection whose session an older server has ended, so
// that the next use connects again.
func (c *Client) forget(conn *sdk.ClientSession) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
	go conn.Close()
}

// Close closes the connection, if there is one, which ends the session of
// an older server.
func (c *Client) Close() error {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

// with does something with the connection, connecting first if need be,
// and once more if an older server has ended its session.
func (c *Client) with(ctx context.Context, do func(*sdk.ClientSession) error) error {
	for attempt := 0; ; attempt++ {
		conn, err := c.connected(ctx)
		if err != nil {
			return err
		}
		err = do(conn)
		if errors.Is(err, sdk.ErrSessionMissing) && attempt == 0 {
			c.forget(conn)
			continue
		}
		return err
	}
}

// Call calls what the request is for, which is for the server of the
// client. A JSON-RPC error is returned as a Response, not an error.
func (c *Client) Call(ctx context.Context, r Request) (*Response, error) {
	if r.Name == "" {
		return nil, errors.New("no tool, resource or prompt is chosen")
	}
	arguments, err := r.arguments()
	if err != nil {
		return nil, err
	}

	answer := &answer{}
	ctx = context.WithValue(ctx, answerKey{}, answer)
	start := time.Now()

	var result any
	err = c.with(ctx, func(conn *sdk.ClientSession) error {
		var err error
		switch r.Kind {
		case KindTool, "":
			result, err = conn.CallTool(ctx, &sdk.CallToolParams{Name: r.Name, Arguments: arguments})
		case KindPrompt:
			params := &sdk.GetPromptParams{Name: r.Name}
			if arguments != nil {
				if err := json.Unmarshal(arguments, &params.Arguments); err != nil {
					return fmt.Errorf("the arguments of a prompt are not all text: %w", err)
				}
			}
			result, err = conn.GetPrompt(ctx, params)
		case KindResource:
			result, err = conn.ReadResource(ctx, &sdk.ReadResourceParams{URI: r.Name})
		default:
			err = fmt.Errorf("%q is not a kind of thing to call", r.Kind)
		}
		return err
	})

	res := &Response{Duration: time.Since(start)}
	res.Status, res.Headers = answer.get()
	if conn := c.current(); conn != nil {
		res.Protocol, res.Server = describe(conn.InitializeResult())
	}

	var rpcError *jsonrpc.Error
	switch {
	case errors.As(err, &rpcError):
		res.Error = rpcError
		return res, nil
	case err != nil:
		return nil, err
	}
	if res.Result, err = json.Marshal(result); err != nil {
		return nil, err
	}
	return res, nil
}

// current returns the connection to the server, or nil if there is none.
func (c *Client) current() *sdk.ClientSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn
}

// describe returns the version of the protocol a connection agreed on, and
// the name and version the server gave, from what the server answered to
// server/discover, or to initialize if it is older.
func describe(info *sdk.InitializeResult) (protocol, server string) {
	if info == nil {
		return "", ""
	}
	if info.ServerInfo != nil {
		server = strings.TrimSpace(info.ServerInfo.Name + " " + info.ServerInfo.Version)
	}
	return info.ProtocolVersion, server
}

// headers is the transport of the HTTP requests made to a server, which
// gives them the headers and authorization of the request, and records the
// status and headers of the answer to a call.
type headers struct {
	base    http.RoundTripper
	request Request
}

func (h *headers) RoundTrip(req *http.Request) (*http.Response, error) {
	// Building the request of a REST request gives the headers the same
	// precedence: the authorization, then the headers of the request.
	built, err := rest.Request{Method: req.Method, URL: req.URL.String(), Headers: h.request.Headers, Auth: h.request.Auth}.Build(req.Context())
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	for key, values := range built.Header {
		req.Header[key] = values
	}
	if built.Host != "" {
		req.Host = built.Host
	}

	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	res, err := base.RoundTrip(req)
	if err == nil && req.Method == http.MethodPost {
		if answer, ok := req.Context().Value(answerKey{}).(*answer); ok {
			answer.set(res.Status, res.Header.Clone())
		}
	}
	return res, err
}

// answerKey is the key of the answer of a call in the context of its HTTP
// requests.
type answerKey struct{}

// answer is the status and headers of the HTTP response to a call.
type answer struct {
	mu      sync.Mutex
	status  string
	headers http.Header
}

func (a *answer) set(status string, headers http.Header) {
	a.mu.Lock()
	a.status, a.headers = status, headers
	a.mu.Unlock()
}

func (a *answer) get() (string, http.Header) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status, a.headers
}
