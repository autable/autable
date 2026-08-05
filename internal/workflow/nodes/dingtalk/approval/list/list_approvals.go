package list

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

// DingTalk caps a page at 20 instances.
const maxPageSize = 20

type dingTalkWorkflowClient interface {
	QueryAllProcessInstancesWithOptions(request *dingworkflow.QueryAllProcessInstancesRequest, headers *dingworkflow.QueryAllProcessInstancesHeaders, runtime *util.RuntimeOptions) (*dingworkflow.QueryAllProcessInstancesResponse, error)
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
		Type:          "dingtalk.approval.list",
		DisplayName:   "DingTalk approvals",
		Description:   "Pages approval instances of one template out of DingTalk, with each instance's form values flattened to a field map.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "start_time", Type: "int", Description: "Start of the window as a millisecond timestamp; instances created at or after it are returned."},
			{Name: "end_time", Type: "int", Description: "Optional end of the window as a millisecond timestamp; defaults to open-ended."},
			{Name: "next_token", Type: "string", Description: "Cursor from a previous page; omit for the first page."},
			{Name: "limit", Type: "int", Description: "Optional page size, 1-20; defaults to the DingTalk maximum of 20."},
			{Name: "process_code", Type: "string", Description: "Optional approval template code, overrides the process_code variable."},
		},
		Outputs: []workflow.Port{
			{Name: "instances", Type: "object[]", Description: "Approval instances; each carries instance_id, title, status, result, originator, timestamps, the form values flattened into values, and the raw form_values, tasks and operation_records."},
			{Name: "count", Type: "int", Description: "Instances in this page."},
			{Name: "next_token", Type: "string", Description: "Cursor for the next page; empty when the last page was returned."},
			{Name: "has_more", Type: "bool", Description: "True when another page remains."},
		},
		Variables: []workflow.Port{
			{Name: "process_code", Type: "string", Description: "Approval template code, e.g. PROC-xxxx."},
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
	processCode := strings.TrimSpace(stringInput(input, "process_code"))
	if processCode == "" {
		processCode = strings.TrimSpace(info.Variables["process_code"])
	}
	if processCode == "" {
		return nil, errors.New("dingtalk process_code is required as an input or variable")
	}
	startTime, err := requiredMillisInput(input, "start_time")
	if err != nil {
		return nil, err
	}
	endTime, hasEndTime, err := optionalMillisInput(input, "end_time")
	if err != nil {
		return nil, err
	}
	if hasEndTime && endTime < startTime {
		return nil, errors.New("end_time must not be before start_time")
	}
	pageSize, err := pageSizeInput(input)
	if err != nil {
		return nil, err
	}

	accessToken, err := node.accessToken(ctx, appKey, appSecret)
	if err != nil {
		return nil, err
	}
	request := (&dingworkflow.QueryAllProcessInstancesRequest{}).
		SetProcessCode(processCode).
		SetStartTimeInMills(startTime).
		SetMaxResults(pageSize)
	if hasEndTime {
		request.SetEndTimeInMills(endTime)
	}
	if token := strings.TrimSpace(stringInput(input, "next_token")); token != "" {
		request.SetNextToken(token)
	}
	response, err := node.workflowClient.QueryAllProcessInstancesWithOptions(
		request,
		(&dingworkflow.QueryAllProcessInstancesHeaders{}).SetXAcsDingtalkAccessToken(accessToken),
		&util.RuntimeOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("dingtalk query process instances: %w", err)
	}
	if response == nil || response.Body == nil || response.Body.Result == nil {
		return nil, errors.New("dingtalk query process instances returned no result")
	}

	result := response.Body.Result
	instances := make([]map[string]any, 0, len(result.List))
	for _, item := range result.List {
		if item == nil {
			continue
		}
		instances = append(instances, instanceOutput(item))
	}
	return map[string]any{
		"instances":  instances,
		"count":      len(instances),
		"next_token": derefString(result.NextToken),
		"has_more":   result.HasMore != nil && *result.HasMore,
	}, nil
}

func instanceOutput(item *dingworkflow.QueryAllProcessInstancesResponseBodyResultList) map[string]any {
	formValues := make([]map[string]any, 0, len(item.FormComponentValues))
	values := map[string]any{}
	for _, component := range item.FormComponentValues {
		if component == nil {
			continue
		}
		name := strings.TrimSpace(derefString(component.Name))
		value := derefString(component.Value)
		formValues = append(formValues, map[string]any{
			"name":      name,
			"value":     value,
			"id":        derefString(component.Id),
			"ext_value": derefString(component.ExtValue),
		})
		if name == "" {
			continue
		}
		// A template may repeat a component name; the later one would silently
		// overwrite the earlier value in the flat map, so the first one wins.
		if _, taken := values[name]; !taken {
			values[name] = value
		}
	}

	tasks := make([]map[string]any, 0, len(item.Tasks))
	for _, task := range item.Tasks {
		if task == nil {
			continue
		}
		tasks = append(tasks, map[string]any{
			"task_id":     derefInt64(task.TaskId),
			"user_id":     derefString(task.UserId),
			"status":      derefString(task.Status),
			"result":      derefString(task.Result),
			"create_time": derefInt64(task.CreateTimestamp),
			"finish_time": derefInt64(task.FinishTimestamp),
		})
	}

	records := make([]map[string]any, 0, len(item.OperationRecords))
	for _, record := range item.OperationRecords {
		if record == nil {
			continue
		}
		records = append(records, map[string]any{
			"user_id":   derefString(record.UserId),
			"type":      derefString(record.OperationType),
			"result":    derefString(record.Result),
			"remark":    derefString(record.Remark),
			"timestamp": derefInt64(record.Timestamp),
		})
	}

	return map[string]any{
		"instance_id":        derefString(item.ProcessInstanceId),
		"business_id":        derefString(item.BusinessId),
		"title":              derefString(item.Title),
		"status":             derefString(item.Status),
		"result":             derefString(item.Result),
		"originator_user_id": derefString(item.OriginatorUserid),
		"originator_dept_id": derefString(item.OriginatorDeptId),
		"create_time":        derefInt64(item.CreateTime),
		"finish_time":        derefInt64(item.FinishTime),
		"values":             values,
		"form_values":        formValues,
		"tasks":              tasks,
		"operation_records":  records,
	}
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

func pageSizeInput(input map[string]any) (int64, error) {
	value, present, err := optionalIntInput(input, "limit")
	if err != nil {
		return 0, err
	}
	if !present {
		return maxPageSize, nil
	}
	if value < 1 || value > maxPageSize {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxPageSize)
	}
	return value, nil
}

func requiredMillisInput(input map[string]any, key string) (int64, error) {
	value, present, err := optionalIntInput(input, key)
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, fmt.Errorf("%s is required", key)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must not be negative", key)
	}
	return value, nil
}

func optionalMillisInput(input map[string]any, key string) (int64, bool, error) {
	value, present, err := optionalIntInput(input, key)
	if err != nil {
		return 0, false, err
	}
	if !present {
		return 0, false, nil
	}
	if value < 0 {
		return 0, false, fmt.Errorf("%s must not be negative", key)
	}
	return value, true, nil
}

func optionalIntInput(input map[string]any, key string) (int64, bool, error) {
	raw, ok := input[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	switch typed := raw.(type) {
	case int:
		return int64(typed), true, nil
	case int64:
		return typed, true, nil
	case float64:
		if float64(int64(typed)) != typed {
			return 0, false, fmt.Errorf("%s must be a whole number, got %v", key, raw)
		}
		return int64(typed), true, nil
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false, fmt.Errorf("%s: %w", key, err)
		}
		return parsed, true, nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false, nil
		}
		parsed, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("%s: %w", key, err)
		}
		return parsed, true, nil
	default:
		return 0, false, fmt.Errorf("%s must be a number, got %T", key, raw)
	}
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

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func stringPtr(value string) *string {
	return &value
}

var _ workflow.Node = Node{}
