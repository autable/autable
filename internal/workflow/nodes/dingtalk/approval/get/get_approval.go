package get

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
	GetProcessInstanceWithOptions(request *dingworkflow.GetProcessInstanceRequest, headers *dingworkflow.GetProcessInstanceHeaders, runtime *util.RuntimeOptions) (*dingworkflow.GetProcessInstanceResponse, error)
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
		Type:          "dingtalk.approval.get",
		DisplayName:   "DingTalk approval detail",
		Description:   "Reads one DingTalk approval instance by id, with its form values flattened to a field map.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "instance_id", Type: "string", Description: "The approval instance id to read."},
		},
		Outputs: []workflow.Port{
			{Name: "instance_id", Type: "string", Description: "The instance id that was read."},
			{Name: "status", Type: "string", Description: "NEW, RUNNING, TERMINATED or COMPLETED."},
			{Name: "result", Type: "string", Description: "agree or refuse once the instance is COMPLETED, otherwise empty."},
			{Name: "title", Type: "string", Description: "Instance title as shown in DingTalk."},
			{Name: "business_id", Type: "string", Description: "Business id DingTalk assigned to the instance."},
			{Name: "originator_user_id", Type: "string", Description: "User id of the initiator."},
			{Name: "originator_dept_id", Type: "string", Description: "Department id of the initiator."},
			{Name: "originator_dept_name", Type: "string", Description: "Department name of the initiator."},
			{Name: "create_time", Type: "string", Description: "Creation time exactly as DingTalk formats it; see the documentation about its trailing Z."},
			{Name: "finish_time", Type: "string", Description: "Completion time in the same format, empty while the instance is running."},
			{Name: "values", Type: "object", Description: "Form components flattened to a {field name: value} map."},
			{Name: "form_values", Type: "object[]", Description: "Form components as DingTalk returned them, with id, name, value, ext_value, component_type and biz_alias."},
			{Name: "tasks", Type: "object[]", Description: "Approval tasks with task_id, user_id, status, result, activity_id and timestamps."},
			{Name: "operation_records", Type: "object[]", Description: "Operation records with user_id, show_name, type, result, remark and date."},
			{Name: "cc_user_ids", Type: "string[]", Description: "User ids the instance was copied to."},
			{Name: "attached_instance_ids", Type: "string[]", Description: "Ids of instances attached to this one."},
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

	accessToken, err := node.accessToken(ctx, appKey, appSecret)
	if err != nil {
		return nil, err
	}
	response, err := node.workflowClient.GetProcessInstanceWithOptions(
		(&dingworkflow.GetProcessInstanceRequest{}).SetProcessInstanceId(instanceID),
		(&dingworkflow.GetProcessInstanceHeaders{}).SetXAcsDingtalkAccessToken(accessToken),
		&util.RuntimeOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("dingtalk get process instance %s: %w", instanceID, err)
	}
	if response == nil || response.Body == nil || response.Body.Result == nil {
		return nil, fmt.Errorf("dingtalk get process instance %s returned no result", instanceID)
	}

	return instanceOutput(instanceID, response.Body.Result), nil
}

func instanceOutput(instanceID string, result *dingworkflow.GetProcessInstanceResponseBodyResult) map[string]any {
	formValues := make([]map[string]any, 0, len(result.FormComponentValues))
	values := map[string]any{}
	for _, component := range result.FormComponentValues {
		if component == nil {
			continue
		}
		name := strings.TrimSpace(derefString(component.Name))
		value := derefString(component.Value)
		formValues = append(formValues, map[string]any{
			"name":           name,
			"value":          value,
			"id":             derefString(component.Id),
			"ext_value":      derefString(component.ExtValue),
			"component_type": derefString(component.ComponentType),
			"biz_alias":      derefString(component.BizAlias),
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

	tasks := make([]map[string]any, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		if task == nil {
			continue
		}
		tasks = append(tasks, map[string]any{
			"task_id":     derefInt64(task.TaskId),
			"user_id":     derefString(task.UserId),
			"status":      derefString(task.Status),
			"result":      derefString(task.Result),
			"activity_id": derefString(task.ActivityId),
			"create_time": derefString(task.CreateTime),
			"finish_time": derefString(task.FinishTime),
		})
	}

	records := make([]map[string]any, 0, len(result.OperationRecords))
	for _, record := range result.OperationRecords {
		if record == nil {
			continue
		}
		records = append(records, map[string]any{
			"user_id":     derefString(record.UserId),
			"show_name":   derefString(record.ShowName),
			"type":        derefString(record.Type),
			"result":      derefString(record.Result),
			"remark":      derefString(record.Remark),
			"activity_id": derefString(record.ActivityId),
			"date":        derefString(record.Date),
		})
	}

	return map[string]any{
		"instance_id":           instanceID,
		"status":                derefString(result.Status),
		"result":                derefString(result.Result),
		"title":                 derefString(result.Title),
		"business_id":           derefString(result.BusinessId),
		"originator_user_id":    derefString(result.OriginatorUserId),
		"originator_dept_id":    derefString(result.OriginatorDeptId),
		"originator_dept_name":  derefString(result.OriginatorDeptName),
		"create_time":           derefString(result.CreateTime),
		"finish_time":           derefString(result.FinishTime),
		"values":                values,
		"form_values":           formValues,
		"tasks":                 tasks,
		"operation_records":     records,
		"cc_user_ids":           stringList(result.CcUserIds),
		"attached_instance_ids": stringList(result.AttachedProcessInstanceIds),
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

func stringInput(input map[string]any, key string) string {
	if raw, ok := input[key]; ok && raw != nil {
		if text, ok := raw.(string); ok {
			return text
		}
		return fmt.Sprint(raw)
	}
	return ""
}

func stringList(values []*string) []string {
	list := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			continue
		}
		list = append(list, *value)
	}
	return list
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
