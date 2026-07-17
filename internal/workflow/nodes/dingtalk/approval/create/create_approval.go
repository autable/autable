package create

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	oauth2 "github.com/alibabacloud-go/dingtalk/oauth2_1_0"
	dingworkflow "github.com/alibabacloud-go/dingtalk/workflow_1_0"
	util "github.com/alibabacloud-go/tea-utils/v2/service"

	"autable/internal/workflow"
)

type dingTalkWorkflowClient interface {
	StartProcessInstanceWithOptions(request *dingworkflow.StartProcessInstanceRequest, headers *dingworkflow.StartProcessInstanceHeaders, runtime *util.RuntimeOptions) (*dingworkflow.StartProcessInstanceResponse, error)
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
		Type:          "dingtalk.approval.create",
		DisplayName:   "DingTalk approval",
		Description:   "Starts a DingTalk OA approval instance from a process template, filling the form from the workflow.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "form_values", Type: "object[]", Description: "Form component values as {name, value} pairs matching the approval template; non-string values are serialized."},
			{Name: "originator_user_id", Type: "string", Description: "Optional DingTalk userId of the initiator, overrides the originator_user_id variable."},
			{Name: "dept_id", Type: "int", Description: "Optional department the instance is started in, overrides the dept_id variable; -1 uses the initiator's main department."},
		},
		Outputs: []workflow.Port{
			{Name: "instance_id", Type: "string", Description: "The created approval instance ID."},
		},
		Variables: []workflow.Port{
			{Name: "process_code", Type: "string", Description: "Approval template code, e.g. PROC-xxxx."},
			{Name: "originator_user_id", Type: "string", Description: "Default DingTalk userId of the initiator."},
			{Name: "dept_id", Type: "string", Description: "Optional default department ID."},
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
	processCode := strings.TrimSpace(info.Variables["process_code"])
	if processCode == "" {
		return nil, errors.New("dingtalk process_code variable is required")
	}
	originator := strings.TrimSpace(stringInput(input, "originator_user_id"))
	if originator == "" {
		originator = strings.TrimSpace(info.Variables["originator_user_id"])
	}
	if originator == "" {
		return nil, errors.New("dingtalk originator_user_id is required as an input or variable")
	}
	deptID, hasDeptID, err := deptIDValue(input, info)
	if err != nil {
		return nil, err
	}
	formValues, err := formComponentValues(input["form_values"])
	if err != nil {
		return nil, err
	}

	accessToken, err := node.accessToken(ctx, appKey, appSecret)
	if err != nil {
		return nil, err
	}
	request := (&dingworkflow.StartProcessInstanceRequest{}).
		SetProcessCode(processCode).
		SetOriginatorUserId(originator).
		SetFormComponentValues(formValues)
	if hasDeptID {
		request.SetDeptId(deptID)
	}
	response, err := node.workflowClient.StartProcessInstanceWithOptions(
		request,
		(&dingworkflow.StartProcessInstanceHeaders{}).SetXAcsDingtalkAccessToken(accessToken),
		&util.RuntimeOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("dingtalk start process instance: %w", err)
	}
	if response == nil || response.Body == nil || response.Body.InstanceId == nil || *response.Body.InstanceId == "" {
		return nil, errors.New("dingtalk start process instance returned no instance id")
	}
	return map[string]any{"instance_id": *response.Body.InstanceId}, nil
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
		return "", err
	}
	if response == nil || response.Body == nil || response.Body.AccessToken == nil || strings.TrimSpace(*response.Body.AccessToken) == "" {
		return "", errors.New("dingtalk access token response is empty")
	}
	return strings.TrimSpace(*response.Body.AccessToken), nil
}

func formComponentValues(value any) ([]*dingworkflow.StartProcessInstanceRequestFormComponentValues, error) {
	entries, ok := value.([]any)
	if !ok || len(entries) == 0 {
		return nil, errors.New("dingtalk form_values must be a non-empty array of {name, value} objects")
	}
	values := make([]*dingworkflow.StartProcessInstanceRequestFormComponentValues, 0, len(entries))
	for index, entry := range entries {
		raw, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("dingtalk form_values[%d] must be an object", index)
		}
		name, _ := raw["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("dingtalk form_values[%d].name is required", index)
		}
		text, err := formValueText(raw["value"])
		if err != nil {
			return nil, fmt.Errorf("dingtalk form_values[%d].value: %w", index, err)
		}
		values = append(values, (&dingworkflow.StartProcessInstanceRequestFormComponentValues{}).
			SetName(name).
			SetValue(text))
	}
	return values, nil
}

// formValueText renders a form value the way the approval API expects:
// strings pass through, scalars are printed, and structured values (detail
// tables, multi-selects) are JSON-encoded.
func formValueText(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", errors.New("value is required")
	case string:
		return typed, nil
	case bool, int, int32, int64, float32, float64:
		return fmt.Sprint(typed), nil
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	}
}

func deptIDValue(input map[string]any, info workflow.RuntimeInfo) (int64, bool, error) {
	switch typed := input["dept_id"].(type) {
	case nil:
	case int:
		return int64(typed), true, nil
	case int64:
		return typed, true, nil
	case float64:
		return int64(typed), true, nil
	default:
		return 0, false, fmt.Errorf("dingtalk dept_id input must be a number, got %T", typed)
	}
	text := strings.TrimSpace(info.Variables["dept_id"])
	if text == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("dingtalk dept_id variable must be a number, got %q", text)
	}
	return parsed, true, nil
}

func stringInput(input map[string]any, key string) string {
	if value, ok := input[key].(string); ok {
		return value
	}
	return ""
}

func stringPtr(value string) *string {
	return &value
}

var _ workflow.Node = Node{}
