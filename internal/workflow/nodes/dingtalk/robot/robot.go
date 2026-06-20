package robot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"autable/internal/workflow"
)

const dingtalkRobotEndpoint = "https://oapi.dingtalk.com/robot/send"

type Node struct {
	client   *http.Client
	endpoint string
}

func NewNode() Node {
	return Node{
		client:   http.DefaultClient,
		endpoint: dingtalkRobotEndpoint,
	}
}

func NewNodeForTest(client *http.Client, endpoint string) Node {
	node := NewNode()
	if client != nil {
		node.client = client
	}
	if endpoint != "" {
		node.endpoint = endpoint
	}
	return node
}

func (node Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "dingtalk.robot.send",
		DisplayName:   "DingTalk robot",
		Description:   "Sends a text message through a DingTalk custom robot webhook.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "content", Type: "string", Description: "Text content to send."},
			{Name: "at_user_ids", Type: "string[]", Description: "Optional DingTalk user IDs to mention."},
			{Name: "at_all", Type: "boolean", Description: "Mention everyone in the group."},
		},
		Outputs: []workflow.Port{
			{Name: "status_code", Type: "int"},
			{Name: "response", Type: "object"},
			{Name: "errcode", Type: "number"},
			{Name: "errmsg", Type: "string"},
		},
		Secrets: []workflow.Port{
			{Name: "access_token", Type: "string", Description: "DingTalk custom robot access_token."},
		},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	accessToken := strings.TrimSpace(info.Secrets["access_token"])
	if accessToken == "" {
		return nil, errors.New("dingtalk access_token secret is required")
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

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, node.webhookURL(accessToken), bytes.NewReader(bodyBytes))
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

func (node Node) webhookURL(accessToken string) string {
	parsed, err := url.Parse(node.endpoint)
	if err != nil {
		return node.endpoint
	}
	values := parsed.Query()
	values.Set("access_token", accessToken)
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

var _ workflow.Node = Node{}
