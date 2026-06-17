package workflow

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const dingtalkRobotEndpoint = "https://oapi.dingtalk.com/robot/send"

type DingTalkRobotNode struct {
	client   *http.Client
	endpoint string
	now      func() time.Time
}

func NewDingTalkRobotNode() DingTalkRobotNode {
	return DingTalkRobotNode{
		client:   http.DefaultClient,
		endpoint: dingtalkRobotEndpoint,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func NewDingTalkRobotNodeForTest(client *http.Client, endpoint string, now func() time.Time) DingTalkRobotNode {
	node := NewDingTalkRobotNode()
	if client != nil {
		node.client = client
	}
	if endpoint != "" {
		node.endpoint = endpoint
	}
	if now != nil {
		node.now = now
	}
	return node
}

func (node DingTalkRobotNode) Info() NodeInfo {
	return NodeInfo{
		Type:        "dingtalk.robot.send",
		DisplayName: "DingTalk robot",
		Description: "Sends a signed text message through a DingTalk custom robot webhook.",
		Inputs: []Port{
			{Name: "content", Type: "string", Description: "Text content to send."},
			{Name: "at_user_ids", Type: "string[]", Description: "Optional DingTalk user IDs to mention."},
			{Name: "at_all", Type: "boolean", Description: "Mention everyone in the group."},
		},
		Outputs: []Port{
			{Name: "status_code", Type: "int"},
			{Name: "response", Type: "object"},
			{Name: "errcode", Type: "number"},
			{Name: "errmsg", Type: "string"},
		},
		Secrets: []Port{
			{Name: "access_token", Type: "string", Description: "DingTalk custom robot access_token."},
			{Name: "secret", Type: "string", Description: "DingTalk custom robot signing secret."},
		},
		Stateless: true,
	}
}

func (node DingTalkRobotNode) Run(ctx context.Context, input map[string]any, info RuntimeInfo) (map[string]any, error) {
	accessToken := strings.TrimSpace(info.Secrets["access_token"])
	secret := strings.TrimSpace(info.Secrets["secret"])
	if accessToken == "" {
		return nil, errors.New("dingtalk access_token secret is required")
	}
	if secret == "" {
		return nil, errors.New("dingtalk secret secret is required")
	}
	content := strings.TrimSpace(stringInput(input, "content"))
	if content == "" {
		return nil, errors.New("dingtalk content is required")
	}

	body := map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": content,
		},
		"at": map[string]any{
			"atUserIds": stringSliceInput(input, "at_user_ids"),
			"isAtAll":   boolInput(input, "at_all"),
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, node.signedURL(accessToken, secret), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := node.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var responseBody map[string]any
	if len(responseBytes) > 0 {
		if err := json.Unmarshal(responseBytes, &responseBody); err != nil {
			responseBody = map[string]any{"body": string(responseBytes)}
		}
	} else {
		responseBody = map[string]any{}
	}
	output := map[string]any{
		"status_code": response.StatusCode,
		"response":    responseBody,
	}
	if errcode, ok := responseBody["errcode"]; ok {
		output["errcode"] = errcode
	}
	if errmsg, ok := responseBody["errmsg"]; ok {
		output["errmsg"] = errmsg
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return output, fmt.Errorf("dingtalk robot request failed: status %d", response.StatusCode)
	}
	return output, nil
}

func (node DingTalkRobotNode) signedURL(accessToken, secret string) string {
	timestamp := node.now().UTC().UnixMilli()
	signPayload := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signPayload))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	parsed, err := url.Parse(node.endpoint)
	if err != nil {
		return node.endpoint
	}
	values := parsed.Query()
	values.Set("access_token", accessToken)
	values.Set("timestamp", fmt.Sprintf("%d", timestamp))
	values.Set("sign", sign)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func stringInput(input map[string]any, key string) string {
	if value, ok := input[key].(string); ok {
		return value
	}
	return ""
}

func boolInput(input map[string]any, key string) bool {
	if value, ok := input[key].(bool); ok {
		return value
	}
	return false
}

func stringSliceInput(input map[string]any, key string) []string {
	value, ok := input[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

var _ Node = DingTalkRobotNode{}
