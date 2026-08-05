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

const (
	editionStandard = "standard"
	editionPremium  = "premium"
)

type dingTalkWorkflowClient interface {
	QueryAllProcessInstancesWithOptions(request *dingworkflow.QueryAllProcessInstancesRequest, headers *dingworkflow.QueryAllProcessInstancesHeaders, runtime *util.RuntimeOptions) (*dingworkflow.QueryAllProcessInstancesResponse, error)
	PremiumGetProcessInstancesWithOptions(request *dingworkflow.PremiumGetProcessInstancesRequest, headers *dingworkflow.PremiumGetProcessInstancesHeaders, runtime *util.RuntimeOptions) (*dingworkflow.PremiumGetProcessInstancesResponse, error)
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
			{Name: "edition", Type: "string", Description: "Optional DingTalk approval edition, standard or premium; overrides the edition variable."},
		},
		Outputs: []workflow.Port{
			{Name: "instances", Type: "object[]", Description: "Approval instances; each carries instance_id, title, status, result, originator, timestamps, the form values flattened into values, and the raw form_values, tasks and operation_records."},
			{Name: "count", Type: "int", Description: "Instances in this page."},
			{Name: "next_token", Type: "string", Description: "Cursor for the next page; empty when the last page was returned."},
			{Name: "has_more", Type: "bool", Description: "True when another page remains."},
		},
		Variables: []workflow.Port{
			{Name: "process_code", Type: "string", Description: "Approval template code, e.g. PROC-xxxx."},
			{Name: "edition", Type: "string", Description: "DingTalk approval edition: standard (default) or premium for orgs on the premium approval plan, which is served by a separate endpoint."},
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
	edition, err := editionSetting(input, info)
	if err != nil {
		return nil, err
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
	nextToken := strings.TrimSpace(stringInput(input, "next_token"))

	accessToken, err := node.accessToken(ctx, appKey, appSecret)
	if err != nil {
		return nil, err
	}

	query := instanceQuery{
		processCode: processCode,
		startTime:   startTime,
		endTime:     endTime,
		hasEndTime:  hasEndTime,
		pageSize:    pageSize,
		nextToken:   nextToken,
		accessToken: accessToken,
	}
	var page instancePage
	if edition == editionPremium {
		page, err = node.premiumPage(query)
	} else {
		page, err = node.standardPage(query)
	}
	if err != nil {
		return nil, err
	}

	instances := make([]map[string]any, 0, len(page.instances))
	for _, item := range page.instances {
		instances = append(instances, instanceOutput(item))
	}
	return map[string]any{
		"instances":  instances,
		"count":      len(instances),
		"next_token": page.nextToken,
		"has_more":   page.hasMore,
	}, nil
}

type instanceQuery struct {
	processCode string
	startTime   int64
	endTime     int64
	hasEndTime  bool
	pageSize    int64
	nextToken   string
	accessToken string
}

type instancePage struct {
	instances []instanceData
	nextToken string
	hasMore   bool
}

// instanceData is the shape both endpoints are normalized into. The two
// responses carry the same fields under different generated types, so the
// output mapping below stays single-sourced.
type instanceData struct {
	instanceID       string
	businessID       string
	title            string
	status           string
	result           string
	originatorUserID string
	originatorDeptID string
	createTime       int64
	finishTime       int64
	components       []formComponent
	tasks            []taskData
	records          []operationRecord
}

type formComponent struct {
	id       string
	name     string
	value    string
	extValue string
}

type taskData struct {
	taskID     int64
	userID     string
	status     string
	result     string
	createTime int64
	finishTime int64
}

type operationRecord struct {
	userID    string
	kind      string
	result    string
	remark    string
	timestamp int64
}

func (node Node) standardPage(query instanceQuery) (instancePage, error) {
	request := (&dingworkflow.QueryAllProcessInstancesRequest{}).
		SetProcessCode(query.processCode).
		SetStartTimeInMills(query.startTime).
		SetMaxResults(query.pageSize)
	if query.hasEndTime {
		request.SetEndTimeInMills(query.endTime)
	}
	if query.nextToken != "" {
		request.SetNextToken(query.nextToken)
	}
	response, err := node.workflowClient.QueryAllProcessInstancesWithOptions(
		request,
		(&dingworkflow.QueryAllProcessInstancesHeaders{}).SetXAcsDingtalkAccessToken(query.accessToken),
		&util.RuntimeOptions{},
	)
	if err != nil {
		return instancePage{}, fmt.Errorf("dingtalk query process instances: %w", err)
	}
	if response == nil || response.Body == nil || response.Body.Result == nil {
		return instancePage{}, errors.New("dingtalk query process instances returned no result")
	}

	result := response.Body.Result
	page := instancePage{
		nextToken: derefString(result.NextToken),
		hasMore:   result.HasMore != nil && *result.HasMore,
		instances: make([]instanceData, 0, len(result.List)),
	}
	for _, item := range result.List {
		if item == nil {
			continue
		}
		instance := instanceData{
			instanceID:       derefString(item.ProcessInstanceId),
			businessID:       derefString(item.BusinessId),
			title:            derefString(item.Title),
			status:           derefString(item.Status),
			result:           derefString(item.Result),
			originatorUserID: derefString(item.OriginatorUserid),
			originatorDeptID: derefString(item.OriginatorDeptId),
			createTime:       derefInt64(item.CreateTime),
			finishTime:       derefInt64(item.FinishTime),
		}
		for _, component := range item.FormComponentValues {
			if component == nil {
				continue
			}
			instance.components = append(instance.components, formComponent{
				id:       derefString(component.Id),
				name:     derefString(component.Name),
				value:    derefString(component.Value),
				extValue: derefString(component.ExtValue),
			})
		}
		for _, task := range item.Tasks {
			if task == nil {
				continue
			}
			instance.tasks = append(instance.tasks, taskData{
				taskID:     derefInt64(task.TaskId),
				userID:     derefString(task.UserId),
				status:     derefString(task.Status),
				result:     derefString(task.Result),
				createTime: derefInt64(task.CreateTimestamp),
				finishTime: derefInt64(task.FinishTimestamp),
			})
		}
		for _, record := range item.OperationRecords {
			if record == nil {
				continue
			}
			instance.records = append(instance.records, operationRecord{
				userID:    derefString(record.UserId),
				kind:      derefString(record.OperationType),
				result:    derefString(record.Result),
				remark:    derefString(record.Remark),
				timestamp: derefInt64(record.Timestamp),
			})
		}
		page.instances = append(page.instances, instance)
	}
	return page, nil
}

func (node Node) premiumPage(query instanceQuery) (instancePage, error) {
	request := (&dingworkflow.PremiumGetProcessInstancesRequest{}).
		SetProcessCode(query.processCode).
		SetStartTimeInMills(query.startTime).
		SetMaxResults(query.pageSize)
	if query.hasEndTime {
		request.SetEndTimeInMills(query.endTime)
	}
	if query.nextToken != "" {
		request.SetNextToken(query.nextToken)
	}
	response, err := node.workflowClient.PremiumGetProcessInstancesWithOptions(
		request,
		(&dingworkflow.PremiumGetProcessInstancesHeaders{}).SetXAcsDingtalkAccessToken(query.accessToken),
		&util.RuntimeOptions{},
	)
	if err != nil {
		return instancePage{}, fmt.Errorf("dingtalk premium get process instances: %w", err)
	}
	if response == nil || response.Body == nil || response.Body.Result == nil {
		return instancePage{}, errors.New("dingtalk premium get process instances returned no result")
	}

	result := response.Body.Result
	page := instancePage{
		nextToken: derefString(result.NextToken),
		hasMore:   result.HasMore != nil && *result.HasMore,
		instances: make([]instanceData, 0, len(result.List)),
	}
	for _, item := range result.List {
		if item == nil {
			continue
		}
		instance := instanceData{
			instanceID:       derefString(item.ProcessInstanceId),
			businessID:       derefString(item.BusinessId),
			title:            derefString(item.Title),
			status:           derefString(item.Status),
			result:           derefString(item.Result),
			originatorUserID: derefString(item.OriginatorUserid),
			originatorDeptID: derefString(item.OriginatorDeptId),
			// The premium payload repeats the timestamps under an
			// InMills name; either one may be the populated one.
			createTime: firstInt64(item.CreateTime, item.CreateTimeInMills),
			finishTime: firstInt64(item.FinishTime, item.FinishTimeInMills),
		}
		for _, component := range item.FormComponentValues {
			if component == nil {
				continue
			}
			instance.components = append(instance.components, formComponent{
				id:       derefString(component.Id),
				name:     derefString(component.Name),
				value:    derefString(component.Value),
				extValue: derefString(component.ExtValue),
			})
		}
		for _, task := range item.Tasks {
			if task == nil {
				continue
			}
			instance.tasks = append(instance.tasks, taskData{
				taskID:     derefInt64(task.TaskId),
				userID:     derefString(task.UserId),
				status:     derefString(task.Status),
				result:     derefString(task.Result),
				createTime: derefInt64(task.CreateTimestamp),
				finishTime: derefInt64(task.FinishTimestamp),
			})
		}
		for _, record := range item.OperationRecords {
			if record == nil {
				continue
			}
			instance.records = append(instance.records, operationRecord{
				userID:    derefString(record.UserId),
				kind:      derefString(record.OperationType),
				result:    derefString(record.Result),
				remark:    derefString(record.Remark),
				timestamp: derefInt64(record.Timestamp),
			})
		}
		page.instances = append(page.instances, instance)
	}
	return page, nil
}

func instanceOutput(item instanceData) map[string]any {
	formValues := make([]map[string]any, 0, len(item.components))
	values := map[string]any{}
	for _, component := range item.components {
		name := strings.TrimSpace(component.name)
		formValues = append(formValues, map[string]any{
			"name":      name,
			"value":     component.value,
			"id":        component.id,
			"ext_value": component.extValue,
		})
		if name == "" {
			continue
		}
		// A template may repeat a component name; the later one would silently
		// overwrite the earlier value in the flat map, so the first one wins.
		if _, taken := values[name]; !taken {
			values[name] = component.value
		}
	}

	tasks := make([]map[string]any, 0, len(item.tasks))
	for _, task := range item.tasks {
		tasks = append(tasks, map[string]any{
			"task_id":     task.taskID,
			"user_id":     task.userID,
			"status":      task.status,
			"result":      task.result,
			"create_time": task.createTime,
			"finish_time": task.finishTime,
		})
	}

	records := make([]map[string]any, 0, len(item.records))
	for _, record := range item.records {
		records = append(records, map[string]any{
			"user_id":   record.userID,
			"type":      record.kind,
			"result":    record.result,
			"remark":    record.remark,
			"timestamp": record.timestamp,
		})
	}

	return map[string]any{
		"instance_id":        item.instanceID,
		"business_id":        item.businessID,
		"title":              item.title,
		"status":             item.status,
		"result":             item.result,
		"originator_user_id": item.originatorUserID,
		"originator_dept_id": item.originatorDeptID,
		"create_time":        item.createTime,
		"finish_time":        item.finishTime,
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

func editionSetting(input map[string]any, info workflow.RuntimeInfo) (string, error) {
	edition := strings.ToLower(strings.TrimSpace(stringInput(input, "edition")))
	if edition == "" {
		edition = strings.ToLower(strings.TrimSpace(info.Variables["edition"]))
	}
	switch edition {
	case "", editionStandard:
		return editionStandard, nil
	case editionPremium:
		return editionPremium, nil
	default:
		return "", fmt.Errorf("edition must be %q or %q, got %q", editionStandard, editionPremium, edition)
	}
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

func firstInt64(values ...*int64) int64 {
	for _, value := range values {
		if value != nil && *value != 0 {
			return *value
		}
	}
	return 0
}

func stringPtr(value string) *string {
	return &value
}

var _ workflow.Node = Node{}
