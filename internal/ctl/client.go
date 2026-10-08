package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// client talks to the autable HTTP API with a bearer session token.
type client struct {
	server string
	token  string
	http   *http.Client
}

// apiError is a non-2xx response; the message is the server's "error" field
// when it sent one.
type apiError struct {
	Status  int
	Message string
	Body    []byte
}

func (err *apiError) Error() string {
	return fmt.Sprintf("%d %s: %s", err.Status, http.StatusText(err.Status), err.Message)
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute}
}

func normalizeServer(server string) (string, error) {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if server == "" {
		return "", errors.New("server URL is required")
	}
	parsed, err := url.Parse(server)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("server must be an http(s) URL, got %q", server)
	}
	return server, nil
}

// apiPath joins escaped path segments under /api.
func apiPath(segments ...string) string {
	escaped := make([]string, len(segments))
	for index, segment := range segments {
		escaped[index] = url.PathEscape(segment)
	}
	return "/api/" + strings.Join(escaped, "/")
}

// do sends body (marshalled as JSON when not nil) and returns the raw
// response body of a 2xx response.
func (c *client) do(method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, c.server+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return c.send(request)
}

func (c *client) send(request *http.Request) ([]byte, error) {
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		message := strings.TrimSpace(string(data))
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &payload) == nil && payload.Error != "" {
			message = payload.Error
		}
		return nil, &apiError{Status: response.StatusCode, Message: message, Body: data}
	}
	return data, nil
}

// call decodes a JSON response into target.
func (c *client) call(method, path string, body, target any) error {
	data, err := c.do(method, path, body)
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(data, target)
}
