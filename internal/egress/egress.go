// Package egress performs the outbound HTTP requests of workflow `fetch`
// calls, on the server and on runners alike, so both apply the same
// limits. Request and Response are also the runner wire format.
package egress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	// MaxResponseBytes caps a response body.
	MaxResponseBytes = 20 << 20
	// MaxRequestBytes caps a request body.
	MaxRequestBytes = 20 << 20
	// DefaultTimeout applies when a request names none.
	DefaultTimeout = 30 * time.Second
	maxRedirects   = 10
)

// Request is one outbound HTTP request. Headers keep their order and may
// repeat; Body travels base64-encoded in JSON.
type Request struct {
	Method    string      `json:"method"`
	URL       string      `json:"url"`
	Headers   [][2]string `json:"headers,omitempty"`
	Body      []byte      `json:"body,omitempty"`
	TimeoutMs int64       `json:"timeout_ms,omitempty"`
}

// Response is the answer to a Request. Header names are lower-case,
// sorted by name; repeated headers repeat in their received order.
type Response struct {
	Status     int         `json:"status"`
	StatusText string      `json:"status_text"`
	URL        string      `json:"url"`
	Headers    [][2]string `json:"headers"`
	Body       []byte      `json:"body"`
}

// ErrResponseTooLarge reports a body above MaxResponseBytes.
var ErrResponseTooLarge = fmt.Errorf("response body exceeds %d MiB", MaxResponseBytes>>20)

// NewClient returns the client Do expects: it follows at most 10 redirects
// and leaves timeouts to the request context.
func NewClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return nil
		},
	}
}

// Timeout returns the request's timeout, DefaultTimeout when unset.
func (request Request) Timeout() time.Duration {
	if request.TimeoutMs <= 0 {
		return DefaultTimeout
	}
	return time.Duration(request.TimeoutMs) * time.Millisecond
}

// Validate checks the parts Do relies on.
func (request Request) Validate() error {
	parsed, err := url.Parse(request.URL)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", request.URL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("url %q must be http or https", request.URL)
	}
	if parsed.Host == "" {
		return fmt.Errorf("url %q has no host", request.URL)
	}
	if len(request.Body) > MaxRequestBytes {
		return fmt.Errorf("request body exceeds %d MiB", MaxRequestBytes>>20)
	}
	return nil
}

// Do performs the request with its timeout. Any HTTP status is a
// response; transport failures, timeouts and oversized bodies are errors.
func Do(ctx context.Context, client *http.Client, request Request) (Response, error) {
	if err := request.Validate(); err != nil {
		return Response{}, err
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = http.MethodGet
	}
	ctx, cancel := context.WithTimeout(ctx, request.Timeout())
	defer cancel()
	var body io.Reader
	if len(request.Body) > 0 {
		body = bytes.NewReader(request.Body)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, request.URL, body)
	if err != nil {
		return Response{}, err
	}
	for _, header := range request.Headers {
		if strings.EqualFold(header[0], "host") {
			httpRequest.Host = header[1]
			continue
		}
		httpRequest.Header.Add(header[0], header[1])
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Response{}, fmt.Errorf("request to %s timed out after %s", request.URL, request.Timeout())
		}
		return Response{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Response{}, fmt.Errorf("request to %s timed out after %s", request.URL, request.Timeout())
		}
		return Response{}, fmt.Errorf("read response: %w", err)
	}
	if len(data) > MaxResponseBytes {
		return Response{}, ErrResponseTooLarge
	}
	return Response{
		Status:     response.StatusCode,
		StatusText: http.StatusText(response.StatusCode),
		URL:        response.Request.URL.String(),
		Headers:    sortedHeaders(response.Header),
		Body:       data,
	}, nil
}

func sortedHeaders(header http.Header) [][2]string {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	headers := make([][2]string, 0, len(names))
	for _, name := range names {
		for _, value := range header[name] {
			headers = append(headers, [2]string{strings.ToLower(name), value})
		}
	}
	return headers
}
