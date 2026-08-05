package list

import (
	"context"
	"strings"
	"testing"

	oauth2 "github.com/alibabacloud-go/dingtalk/oauth2_1_0"
	dingworkflow "github.com/alibabacloud-go/dingtalk/workflow_1_0"
	util "github.com/alibabacloud-go/tea-utils/v2/service"

	"autable/internal/workflow"
)

type fakeWorkflowClient struct {
	request  *dingworkflow.QueryAllProcessInstancesRequest
	headers  *dingworkflow.QueryAllProcessInstancesHeaders
	response *dingworkflow.QueryAllProcessInstancesResponse
	err      error
}

func (client *fakeWorkflowClient) QueryAllProcessInstancesWithOptions(request *dingworkflow.QueryAllProcessInstancesRequest, headers *dingworkflow.QueryAllProcessInstancesHeaders, _ *util.RuntimeOptions) (*dingworkflow.QueryAllProcessInstancesResponse, error) {
	client.request = request
	client.headers = headers
	return client.response, client.err
}

type fakeAccessTokenClient struct {
	appKey    string
	appSecret string
	response  *oauth2.GetAccessTokenResponse
	err       error
}

func (client *fakeAccessTokenClient) GetAccessToken(request *oauth2.GetAccessTokenRequest) (*oauth2.GetAccessTokenResponse, error) {
	if request != nil {
		if request.AppKey != nil {
			client.appKey = *request.AppKey
		}
		if request.AppSecret != nil {
			client.appSecret = *request.AppSecret
		}
	}
	return client.response, client.err
}

func testInfo() workflow.RuntimeInfo {
	return workflow.RuntimeInfo{
		Variables: map[string]string{"process_code": "PROC-TEST-1"},
		Secrets: map[string]string{
			"app_key":    "key-1",
			"app_secret": "secret-1",
		},
	}
}

func sampleInstance() *dingworkflow.QueryAllProcessInstancesResponseBodyResultList {
	return &dingworkflow.QueryAllProcessInstancesResponseBodyResultList{
		ProcessInstanceId: stringPtr("inst-1"),
		BusinessId:        stringPtr("BIZ-1"),
		Title:             stringPtr("Request from Alice"),
		Status:            stringPtr("COMPLETED"),
		Result:            stringPtr("agree"),
		OriginatorUserid:  stringPtr("alice"),
		OriginatorDeptId:  stringPtr("-1"),
		CreateTime:        int64Ptr(1700000000000),
		FinishTime:        int64Ptr(1700000600000),
		FormComponentValues: []*dingworkflow.QueryAllProcessInstancesResponseBodyResultListFormComponentValues{
			{Id: stringPtr("c1"), Name: stringPtr("Amount"), Value: stringPtr("138.20")},
			{Id: stringPtr("c2"), Name: stringPtr("Reason"), Value: stringPtr("restock"), ExtValue: stringPtr("{}")},
			// DingTalk templates may repeat a component name; the first wins.
			{Id: stringPtr("c3"), Name: stringPtr("Amount"), Value: stringPtr("999")},
			{Id: stringPtr("c4"), Name: stringPtr("  "), Value: stringPtr("unnamed")},
			nil,
		},
		Tasks: []*dingworkflow.QueryAllProcessInstancesResponseBodyResultListTasks{
			{
				TaskId:          int64Ptr(9001),
				UserId:          stringPtr("bob"),
				Status:          stringPtr("COMPLETED"),
				Result:          stringPtr("AGREE"),
				CreateTimestamp: int64Ptr(1700000100000),
				FinishTimestamp: int64Ptr(1700000500000),
			},
			nil,
		},
		OperationRecords: []*dingworkflow.QueryAllProcessInstancesResponseBodyResultListOperationRecords{
			{
				UserId:        stringPtr("bob"),
				OperationType: stringPtr("EXECUTE_TASK_NORMAL"),
				Result:        stringPtr("AGREE"),
				Remark:        stringPtr("ok"),
				Timestamp:     int64Ptr(1700000500000),
			},
			nil,
		},
	}
}

func testClients() (*fakeWorkflowClient, *fakeAccessTokenClient) {
	workflowClient := &fakeWorkflowClient{
		response: &dingworkflow.QueryAllProcessInstancesResponse{
			Body: &dingworkflow.QueryAllProcessInstancesResponseBody{
				Result: &dingworkflow.QueryAllProcessInstancesResponseBodyResult{
					List:      []*dingworkflow.QueryAllProcessInstancesResponseBodyResultList{sampleInstance(), nil},
					NextToken: stringPtr("20"),
					HasMore:   boolPtr(true),
				},
			},
		},
	}
	tokenClient := &fakeAccessTokenClient{
		response: &oauth2.GetAccessTokenResponse{
			Body: (&oauth2.GetAccessTokenResponseBody{}).SetAccessToken("token-1"),
		},
	}
	return workflowClient, tokenClient
}

func int64Ptr(value int64) *int64 { return &value }

func boolPtr(value bool) *bool { return &value }

func TestNodePagesInstancesAndFlattensFormValues(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	output, err := node.Run(context.Background(), map[string]any{
		"start_time": float64(1699999999000),
		"end_time":   "1700009999000",
		"next_token": " 0 ",
		"limit":      5,
	}, testInfo())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if tokenClient.appKey != "key-1" || tokenClient.appSecret != "secret-1" {
		t.Fatalf("unexpected credentials %q/%q", tokenClient.appKey, tokenClient.appSecret)
	}
	if got := derefString(workflowClient.headers.XAcsDingtalkAccessToken); got != "token-1" {
		t.Fatalf("access token header = %q", got)
	}
	request := workflowClient.request
	if got := derefString(request.ProcessCode); got != "PROC-TEST-1" {
		t.Fatalf("process code = %q", got)
	}
	if got := derefInt64(request.StartTimeInMills); got != 1699999999000 {
		t.Fatalf("start time = %d", got)
	}
	if got := derefInt64(request.EndTimeInMills); got != 1700009999000 {
		t.Fatalf("end time = %d", got)
	}
	if got := derefInt64(request.MaxResults); got != 5 {
		t.Fatalf("max results = %d", got)
	}
	if got := derefString(request.NextToken); got != "0" {
		t.Fatalf("next token = %q", got)
	}

	if output["count"] != 1 {
		t.Fatalf("count = %v", output["count"])
	}
	if output["next_token"] != "20" {
		t.Fatalf("next_token = %v", output["next_token"])
	}
	if output["has_more"] != true {
		t.Fatalf("has_more = %v", output["has_more"])
	}

	instances, ok := output["instances"].([]map[string]any)
	if !ok || len(instances) != 1 {
		t.Fatalf("instances = %#v", output["instances"])
	}
	instance := instances[0]
	for key, want := range map[string]any{
		"instance_id":        "inst-1",
		"business_id":        "BIZ-1",
		"title":              "Request from Alice",
		"status":             "COMPLETED",
		"result":             "agree",
		"originator_user_id": "alice",
		"originator_dept_id": "-1",
		"create_time":        int64(1700000000000),
		"finish_time":        int64(1700000600000),
	} {
		if instance[key] != want {
			t.Fatalf("instance[%q] = %#v, want %#v", key, instance[key], want)
		}
	}

	values, ok := instance["values"].(map[string]any)
	if !ok {
		t.Fatalf("values = %#v", instance["values"])
	}
	if len(values) != 2 || values["Amount"] != "138.20" || values["Reason"] != "restock" {
		t.Fatalf("flattened values = %#v", values)
	}

	formValues, ok := instance["form_values"].([]map[string]any)
	if !ok || len(formValues) != 4 {
		t.Fatalf("form_values = %#v", instance["form_values"])
	}
	if formValues[2]["value"] != "999" || formValues[1]["ext_value"] != "{}" {
		t.Fatalf("raw form values lost detail: %#v", formValues)
	}

	tasks, ok := instance["tasks"].([]map[string]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("tasks = %#v", instance["tasks"])
	}
	if tasks[0]["task_id"] != int64(9001) || tasks[0]["user_id"] != "bob" || tasks[0]["finish_time"] != int64(1700000500000) {
		t.Fatalf("task = %#v", tasks[0])
	}

	records, ok := instance["operation_records"].([]map[string]any)
	if !ok || len(records) != 1 {
		t.Fatalf("operation_records = %#v", instance["operation_records"])
	}
	if records[0]["result"] != "AGREE" || records[0]["type"] != "EXECUTE_TASK_NORMAL" || records[0]["timestamp"] != int64(1700000500000) {
		t.Fatalf("operation record = %#v", records[0])
	}
}

func TestNodeDefaultsPageSizeAndOmitsOptionalWindow(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	if _, err := node.Run(context.Background(), map[string]any{
		"start_time":   1699999999000,
		"process_code": "PROC-OVERRIDE",
	}, testInfo()); err != nil {
		t.Fatalf("run: %v", err)
	}

	request := workflowClient.request
	if got := derefInt64(request.MaxResults); got != maxPageSize {
		t.Fatalf("max results = %d, want %d", got, maxPageSize)
	}
	if request.EndTimeInMills != nil {
		t.Fatalf("end time should be omitted, got %d", *request.EndTimeInMills)
	}
	if request.NextToken != nil {
		t.Fatalf("next token should be omitted, got %q", *request.NextToken)
	}
	if got := derefString(request.ProcessCode); got != "PROC-OVERRIDE" {
		t.Fatalf("input process code should override the variable, got %q", got)
	}
}

func TestNodeRejectsBadInput(t *testing.T) {
	for name, testCase := range map[string]struct {
		input   map[string]any
		info    workflow.RuntimeInfo
		message string
	}{
		"missing start time": {
			input:   map[string]any{},
			info:    testInfo(),
			message: "start_time is required",
		},
		"reversed window": {
			input:   map[string]any{"start_time": 200, "end_time": 100},
			info:    testInfo(),
			message: "end_time must not be before start_time",
		},
		"oversized page": {
			input:   map[string]any{"start_time": 1, "limit": maxPageSize + 1},
			info:    testInfo(),
			message: "limit must be between 1 and 20",
		},
		"missing process code": {
			input:   map[string]any{"start_time": 1},
			info:    workflow.RuntimeInfo{Secrets: testInfo().Secrets},
			message: "process_code is required",
		},
		"missing app key": {
			input:   map[string]any{"start_time": 1},
			info:    workflow.RuntimeInfo{Variables: testInfo().Variables},
			message: "app_key secret is required",
		},
	} {
		t.Run(name, func(t *testing.T) {
			workflowClient, tokenClient := testClients()
			node := NewNodeForTest(workflowClient, tokenClient)
			_, err := node.Run(context.Background(), testCase.input, testCase.info)
			if err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error = %v, want it to mention %q", err, testCase.message)
			}
			if workflowClient.request != nil {
				t.Fatalf("a rejected input still called DingTalk")
			}
		})
	}
}

func TestNodeInfoDocumentsBothLanguages(t *testing.T) {
	info := NewNodeForTest(nil, nil).Info()
	if info.Type != "dingtalk.approval.list" {
		t.Fatalf("type = %q", info.Type)
	}
	for _, language := range []string{"en-US", "zh-CN"} {
		if strings.TrimSpace(info.Documentation[language]) == "" {
			t.Fatalf("documentation for %s is empty", language)
		}
	}
}
