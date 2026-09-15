package comment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	oauth2 "github.com/alibabacloud-go/dingtalk/oauth2_1_0"
	dingworkflow "github.com/alibabacloud-go/dingtalk/workflow_1_0"
	util "github.com/alibabacloud-go/tea-utils/v2/service"

	"autable/internal/workflow"
)

type dingTalkWorkflowClient interface {
	AddProcessInstanceCommentWithOptions(request *dingworkflow.AddProcessInstanceCommentRequest, headers *dingworkflow.AddProcessInstanceCommentHeaders, runtime *util.RuntimeOptions) (*dingworkflow.AddProcessInstanceCommentResponse, error)
}

type dingTalkAccessTokenClient interface {
	GetAccessToken(request *oauth2.GetAccessTokenRequest) (*oauth2.GetAccessTokenResponse, error)
}

type Node struct {
	workflowClient    dingTalkWorkflowClient
	accessTokenClient dingTalkAccessTokenClient
	clientErr         error
}

func NewNode() Node {
	config := &openapi.Config{
		Protocol: stringPtr("HTTPS"),
	}
	workflowClient, err := dingworkflow.NewClient(config)
	if err != nil {
		return Node{clientErr: err}
	}
	accessTokenClient, err := oauth2.NewClient(config)
	return Node{
		workflowClient:    workflowClient,
		accessTokenClient: accessTokenClient,
		clientErr:         err,
	}
}

func NewNodeForTest(workflowClient dingTalkWorkflowClient, accessTokenClient dingTalkAccessTokenClient) Node {
	return Node{workflowClient: workflowClient, accessTokenClient: accessTokenClient}
}

func (node Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "dingtalk.approval.comment",
		DisplayName:   "DingTalk approval comment",
		Description:   "Posts a text comment onto a DingTalk approval instance on behalf of a user.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "instance_id", Type: "string", Description: "The approval instance to comment on."},
			{Name: "text", Type: "string", Description: "Comment text."},
			{Name: "comment_user_id", Type: "string", Description: "Optional DingTalk user id the comment is posted as; overrides the comment_user_id variable."},
		},
		Outputs: []workflow.Port{
			{Name: "instance_id", Type: "string", Description: "The instance the comment was posted on."},
			{Name: "comment_user_id", Type: "string", Description: "The user id the comment was posted as."},
		},
		Variables: []workflow.Port{
			{Name: "comment_user_id", Type: "string", Description: "DingTalk user id comments are posted as when the input does not name one."},
		},
		Secrets: []workflow.Port{
			{Name: "app_key", Type: "string", Description: "DingTalk OpenAPI app key."},
			{Name: "app_secret", Type: "string", Description: "DingTalk OpenAPI app secret."},
		},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	if node.clientErr != nil {
		return nil, node.clientErr
	}
	if node.workflowClient == nil {
		return nil, errors.New("dingtalk workflow client is not configured")
	}
	if node.accessTokenClient == nil {
		return nil, errors.New("dingtalk access token client is not configured")
	}

	appKey := strings.TrimSpace(info.Secrets["app_key"])
	if appKey == "" {
		return nil, errors.New("dingtalk app_key secret is required")
	}
	appSecret := strings.TrimSpace(info.Secrets["app_secret"])
	if appSecret == "" {
		return nil, errors.New("dingtalk app_secret secret is required")
	}
	instanceID := strings.TrimSpace(stringInput(input, "instance_id"))
	if instanceID == "" {
		return nil, errors.New("instance_id is required")
	}
	text := strings.TrimSpace(stringInput(input, "text"))
	if text == "" {
		return nil, errors.New("text is required")
	}
	commentUserID := strings.TrimSpace(stringInput(input, "comment_user_id"))
	if commentUserID == "" {
		commentUserID = strings.TrimSpace(info.Variables["comment_user_id"])
	}
	if commentUserID == "" {
		return nil, errors.New("dingtalk comment_user_id is required as an input or variable")
	}

	accessToken, err := node.accessToken(ctx, appKey, appSecret)
	if err != nil {
		return nil, err
	}
	response, err := node.workflowClient.AddProcessInstanceCommentWithOptions(
		(&dingworkflow.AddProcessInstanceCommentRequest{}).
			SetProcessInstanceId(instanceID).
			SetText(text).
			SetCommentUserId(commentUserID),
		(&dingworkflow.AddProcessInstanceCommentHeaders{}).SetXAcsDingtalkAccessToken(accessToken),
		&util.RuntimeOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("dingtalk comment on process instance %s: %w", instanceID, err)
	}
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("dingtalk comment on process instance %s returned no result", instanceID)
	}
	if response.Body.Result == nil || !*response.Body.Result {
		return nil, fmt.Errorf("dingtalk did not accept the comment on process instance %s", instanceID)
	}

	return map[string]any{
		"instance_id":     instanceID,
		"comment_user_id": commentUserID,
	}, nil
}

func (node Node) accessToken(ctx context.Context, appKey string, appSecret string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	response, err := node.accessTokenClient.GetAccessToken(
		(&oauth2.GetAccessTokenRequest{}).
			SetAppKey(appKey).
			SetAppSecret(appSecret),
	)
	if err != nil {
		return "", fmt.Errorf("dingtalk access token: %w", err)
	}
	if response == nil || response.Body == nil || response.Body.AccessToken == nil || *response.Body.AccessToken == "" {
		return "", errors.New("dingtalk access token response was empty")
	}
	return *response.Body.AccessToken, nil
}

func stringInput(input map[string]any, key string) string {
	if raw, ok := input[key]; ok && raw != nil {
		if text, ok := raw.(string); ok {
			return text
		}
		return fmt.Sprint(raw)
	}
	return ""
}

func stringPtr(value string) *string {
	return &value
}

var _ workflow.Node = Node{}
