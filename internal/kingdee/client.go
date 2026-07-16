// Package kingdee is a minimal client for the Kingdee K3Cloud WebAPI,
// reimplementing the request signing of the official Python SDK
// (kingdee.cdp.webapi.sdk 8.2.0, MIT) without the SDK's disabled TLS
// verification.
package kingdee

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const billQueryService = "Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.ExecuteBillQuery"

// xorKey deobfuscates the third-party app secret embedded in the app ID; the
// Python SDK derives it from hardcoded segments plus discarded random digits,
// making it this constant (ROT13 of "0054s397p6234378o09pn7q3r5qropr7").
var xorKey = []byte("0054f397c6234378b09ca7d3e5debce7")

type Config struct {
	// ServerURL is the K3Cloud gateway, e.g. https://erp.example.com/K3Cloud.
	ServerURL string
	AcctID    string
	UserName  string
	AppID     string
	AppSecret string
	// LCID defaults to 2052 (simplified Chinese).
	LCID   int
	OrgNum int
	// SkipTLSVerify disables certificate verification for self-signed
	// private cloud gateways. The Python SDK always disables verification;
	// this client verifies by default.
	SkipTLSVerify bool
}

type Client struct {
	config Config
	http   *http.Client
	now    func() time.Time

	mu        sync.Mutex
	sessionID string
	cookies   []string // name=value pairs in receipt order
}

func New(config Config) (*Client, error) {
	if config.ServerURL == "" {
		return nil, errors.New("kingdee server_url is required")
	}
	if config.AcctID == "" || config.UserName == "" || config.AppID == "" || config.AppSecret == "" {
		return nil, errors.New("kingdee acct_id, user_name, app_id, and app_secret are required")
	}
	if config.LCID <= 0 {
		config.LCID = 2052
	}
	transport := http.DefaultTransport
	if config.SkipTLSVerify {
		clone := http.DefaultTransport.(*http.Transport).Clone()
		clone.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		transport = clone
	}
	return &Client{
		config: config,
		http:   &http.Client{Transport: transport, Timeout: 2 * time.Minute},
		now:    time.Now,
	}, nil
}

// ExecuteBillQuery runs the bill query service and returns rows of field
// values in FieldKeys order.
func (client *Client) ExecuteBillQuery(ctx context.Context, data map[string]any) ([][]any, error) {
	response, err := client.Execute(ctx, billQueryService, map[string]any{"data": data})
	if err != nil {
		return nil, err
	}
	var rows [][]any
	if err := json.Unmarshal(response, &rows); err != nil {
		return nil, fmt.Errorf("kingdee bill query returned an unexpected shape: %s", truncate(string(response), 500))
	}
	return rows, nil
}

// Execute posts a service request and returns the raw response body.
func (client *Client) Execute(ctx context.Context, serviceName string, payload map[string]any) ([]byte, error) {
	requestURL := strings.TrimSuffix(client.config.ServerURL, "/") + "/" + serviceName + ".common.kdsvc"
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	client.setHeaders(request, requestURL)

	response, err := client.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("kingdee request failed with status %d: %s", response.StatusCode, truncate(string(responseBody), 500))
	}
	client.storeCookies(response)
	text := string(responseBody)
	if errorText, found := strings.CutPrefix(text, "response_error:"); found {
		errorText = strings.TrimSpace(errorText)
		if errorText == "" {
			errorText = "empty exception message"
		}
		return nil, errors.New("kingdee error: " + errorText)
	}
	return responseBody, nil
}

func (client *Client) setHeaders(request *http.Request, requestURL string) {
	path := requestURL
	if index := strings.Index(requestURL[10:], "/"); index >= 0 {
		path = requestURL[10+index:]
	}
	path = quotePath(path)
	timestamp := strconv.FormatInt(client.now().Unix(), 10)
	nonce := timestamp

	clientID := ""
	clientSecret := ""
	if parts := strings.Split(client.config.AppID, "_"); len(parts) == 2 {
		clientID = parts[0]
		clientSecret = decodeAppSecret(parts[1])
	}
	apiSign := "POST\n" + path + "\n\nx-api-nonce:" + nonce + "\nx-api-timestamp:" + timestamp + "\n"
	appData := client.config.AcctID + "," + client.config.UserName + "," +
		strconv.Itoa(client.config.LCID) + "," + strconv.Itoa(client.config.OrgNum)

	request.Header.Set("X-Api-ClientID", clientID)
	request.Header.Set("X-Api-Auth-Version", "2.0")
	request.Header.Set("x-api-timestamp", timestamp)
	request.Header.Set("x-api-nonce", nonce)
	request.Header.Set("x-api-signheaders", "x-api-timestamp,x-api-nonce")
	request.Header.Set("X-Api-Signature", signHMAC(apiSign, clientSecret))
	request.Header.Set("X-Kd-Appkey", client.config.AppID)
	request.Header.Set("X-Kd-Appdata", base64.StdEncoding.EncodeToString([]byte(appData)))
	request.Header.Set("X-Kd-Signature", signHMAC(client.config.AppID+appData, client.config.AppSecret))
	request.Header.Set("Accept-Charset", "utf-8")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Kingdee/Autable Go WebApi Client")

	client.mu.Lock()
	defer client.mu.Unlock()
	if client.sessionID != "" {
		request.Header.Set("kdservice-sessionid", client.sessionID)
	}
	if len(client.cookies) > 0 {
		request.Header.Set("Cookie", "Theme=standard;"+strings.Join(client.cookies, ";"))
	}
}

func (client *Client) storeCookies(response *http.Response) {
	cookies := response.Cookies()
	if len(cookies) == 0 {
		return
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	client.cookies = client.cookies[:0]
	for _, cookie := range cookies {
		if cookie.Name == "kdservice-sessionid" {
			client.sessionID = cookie.Value
		}
		client.cookies = append(client.cookies, cookie.Name+"="+cookie.Value)
	}
}

// signHMAC matches the Python SDK: base64 of the lowercase hex digest.
func signHMAC(content, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(content))
	return base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(mac.Sum(nil))))
}

// decodeAppSecret deobfuscates the client secret half of the app ID. Invalid
// input yields "" like the Python SDK.
func decodeAppSecret(appSecret string) string {
	if len(appSecret) != 32 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(appSecret)
	if err != nil {
		return ""
	}
	out := make([]byte, len(raw))
	for i := range raw {
		out[i] = raw[i] ^ xorKey[i]
	}
	return base64.StdEncoding.EncodeToString(out)
}

// quotePath matches Python's urllib quote (safe "/", uppercase hex) followed
// by replacing "/" with "%2F"; the result is part of the signed content.
func quotePath(path string) string {
	var builder strings.Builder
	for _, b := range []byte(path) {
		switch {
		case b == '/':
			builder.WriteString("%2F")
		case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9',
			b == '_', b == '.', b == '-', b == '~':
			builder.WriteByte(b)
		default:
			builder.WriteString(fmt.Sprintf("%%%02X", b))
		}
	}
	return builder.String()
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
