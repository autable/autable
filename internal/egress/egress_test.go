package egress

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/echo?moved=1", http.StatusFound)
		case "/echo":
			body, _ := io.ReadAll(r.Body)
			w.Header().Add("Set-Cookie", "a=1")
			w.Header().Add("Set-Cookie", "b=2")
			w.Header().Set("X-Method", r.Method)
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte(r.Header.Get("X-Token") + ":" + string(body) + ":" + r.URL.RawQuery))
		case "/large":
			_, _ = w.Write([]byte(strings.Repeat("x", MaxResponseBytes+1)))
		case "/slow":
			time.Sleep(500 * time.Millisecond)
		}
	}))
	defer server.Close()
	client := NewClient()
	ctx := context.Background()

	response, err := Do(ctx, client, Request{Method: "post", URL: server.URL + "/echo?q=1", Headers: [][2]string{{"X-Token", "t"}}, Body: []byte("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusTeapot || response.StatusText != "I'm a teapot" || string(response.Body) != "t:hi:q=1" {
		t.Fatalf("unexpected response %#v", response)
	}
	cookies := 0
	for _, header := range response.Headers {
		if header[0] == "set-cookie" {
			cookies++
		}
		if header[0] == "x-method" && header[1] != "POST" {
			t.Fatalf("expected the method upper-cased, got %q", header[1])
		}
	}
	if cookies != 2 {
		t.Fatalf("expected repeated headers to repeat, got %#v", response.Headers)
	}

	redirected, err := Do(ctx, client, Request{URL: server.URL + "/redirect"})
	if err != nil || !strings.HasSuffix(redirected.URL, "/echo?moved=1") {
		t.Fatalf("expected the redirect followed, got %#v %v", redirected.URL, err)
	}
	if _, err := Do(ctx, client, Request{URL: server.URL + "/large"}); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("expected the response cap, got %v", err)
	}
	if _, err := Do(ctx, client, Request{URL: server.URL + "/slow", TimeoutMs: 50}); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected a timeout, got %v", err)
	}
	for _, bad := range []string{"ftp://example.com/", "/relative", "http://"} {
		if _, err := Do(ctx, client, Request{URL: bad}); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}
