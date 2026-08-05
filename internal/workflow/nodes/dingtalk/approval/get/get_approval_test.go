package get

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
	request  *dingworkflow.GetProcessInstanceRequest
	headers  *dingworkflow.GetProcessInstanceHeaders
	response *dingworkflow.GetProcessInstanceResponse
	err      error
	calls    int
}

func (client *fakeWorkflowClient) GetProcessInstanceWithOptions(request *dingworkflow.GetProcessInstanceRequest, headers *dingworkflow.GetProcessInstanceHeaders, _ *util.RuntimeOptions) (*dingworkflow.GetProcessInstanceResponse, error) {
	client.request = request
	client.headers = headers
	client.calls++
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
		Secrets: map[string]string{
			"app_key":    "key-1",
			"app_secret": "secret-1",
		},
	}
}

func sampleResult() *dingworkflow.GetProcessInstanceResponseBodyResult {
	return &dingworkflow.GetProcessInstanceResponseBodyResult{
		Status:                     stringPtr("COMPLETED"),
		Result:                     stringPtr("agree"),
		Title:                      stringPtr("Request from Alice"),
		BusinessId:                 stringPtr("BIZ-1"),
		OriginatorUserId:           stringPtr("alice"),
		OriginatorDeptId:           stringPtr("-1"),
		OriginatorDeptName:         stringPtr("Operations"),
		CreateTime:                 stringPtr("2026-08-05T16:58Z"),
		FinishTime:                 stringPtr("2026-08-05T18:28Z"),
		CcUserIds:                  []*string{stringPtr("carol"), nil},
		AttachedProcessInstanceIds: []*string{stringPtr("inst-attached")},
		FormComponentValues: []*dingworkflow.GetProcessInstanceResponseBodyResultFormComponentValues{
			{Id: stringPtr("c1"), Name: stringPtr("Amount"), Value: stringPtr("138.20"), ComponentType: stringPtr("MoneyField"), BizAlias: stringPtr("amount")},
			{Id: stringPtr("c2"), Name: stringPtr("Reason"), Value: stringPtr("restock"), ExtValue: stringPtr("{}")},
			// A template may repeat a component name; the first one wins.
			{Id: stringPtr("c3"), Name: stringPtr("Amount"), Value: stringPtr("999")},
			{Id: stringPtr("c4"), Name: stringPtr("  "), Value: stringPtr("unnamed")},
			nil,
		},
		Tasks: []*dingworkflow.GetProcessInstanceResponseBodyResultTasks{
			{
				TaskId:     int64Ptr(9001),
				UserId:     stringPtr("bob"),
				Status:     stringPtr("COMPLETED"),
				Result:     stringPtr("AGREE"),
				ActivityId: stringPtr("act-1"),
				CreateTime: stringPtr("2026-08-05T17:00Z"),
				FinishTime: stringPtr("2026-08-05T18:28Z"),
			},
			nil,
		},
		OperationRecords: []*dingworkflow.GetProcessInstanceResponseBodyResultOperationRecords{
			{
				UserId:   stringPtr("bob"),
				ShowName: stringPtr("Bob"),
				Type:     stringPtr("EXECUTE_TASK_NORMAL"),
				Result:   stringPtr("AGREE"),
				Remark:   stringPtr("ok"),
				Date:     stringPtr("2026-08-05T18:28Z"),
			},
			nil,
		},
	}
}

func testClients() (*fakeWorkflowClient, *fakeAccessTokenClient) {
	workflowClient := &fakeWorkflowClient{
		response: &dingworkflow.GetProcessInstanceResponse{
			Body: &dingworkflow.GetProcessInstanceResponseBody{Result: sampleResult()},
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

func TestNodeReadsOneInstanceAndFlattensFormValues(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	output, err := node.Run(context.Background(), map[string]any{"instance_id": "  inst-1  "}, testInfo())
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if tokenClient.appKey != "key-1" || tokenClient.appSecret != "secret-1" {
		t.Fatalf("unexpected credentials %q/%q", tokenClient.appKey, tokenClient.appSecret)
	}
	if got := derefString(workflowClient.request.ProcessInstanceId); got != "inst-1" {
		t.Fatalf("instance id sent = %q", got)
	}
	if got := derefString(workflowClient.headers.XAcsDingtalkAccessToken); got != "token-1" {
		t.Fatalf("access token header = %q", got)
	}
	if workflowClient.calls != 1 {
		t.Fatalf("expected exactly one request per instance, got %d", workflowClient.calls)
	}

	for key, want := range map[string]any{
		"instance_id":          "inst-1",
		"status":               "COMPLETED",
		"result":               "agree",
		"title":                "Request from Alice",
		"business_id":          "BIZ-1",
		"originator_user_id":   "alice",
		"originator_dept_id":   "-1",
		"originator_dept_name": "Operations",
		// DingTalk's own formatting is passed through untouched: the trailing
		// Z does not mean UTC, so the node must not reinterpret it.
		"create_time": "2026-08-05T16:58Z",
		"finish_time": "2026-08-05T18:28Z",
	} {
		if output[key] != want {
			t.Fatalf("output[%q] = %#v, want %#v", key, output[key], want)
		}
	}

	values, ok := output["values"].(map[string]any)
	if !ok {
		t.Fatalf("values = %#v", output["values"])
	}
	if len(values) != 2 || values["Amount"] != "138.20" || values["Reason"] != "restock" {
		t.Fatalf("flattened values = %#v", values)
	}

	formValues, ok := output["form_values"].([]map[string]any)
	if !ok || len(formValues) != 4 {
		t.Fatalf("form_values = %#v", output["form_values"])
	}
	if formValues[0]["component_type"] != "MoneyField" || formValues[0]["biz_alias"] != "amount" {
		t.Fatalf("component metadata lost: %#v", formValues[0])
	}
	if formValues[2]["value"] != "999" || formValues[1]["ext_value"] != "{}" {
		t.Fatalf("raw form values lost detail: %#v", formValues)
	}

	tasks, ok := output["tasks"].([]map[string]any)
	if !ok || len(tasks) != 1 {
		t.Fatalf("tasks = %#v", output["tasks"])
	}
	if tasks[0]["task_id"] != int64(9001) || tasks[0]["user_id"] != "bob" || tasks[0]["activity_id"] != "act-1" {
		t.Fatalf("task = %#v", tasks[0])
	}
	if tasks[0]["finish_time"] != "2026-08-05T18:28Z" {
		t.Fatalf("task timestamps = %#v", tasks[0])
	}

	records, ok := output["operation_records"].([]map[string]any)
	if !ok || len(records) != 1 {
		t.Fatalf("operation_records = %#v", output["operation_records"])
	}
	if records[0]["type"] != "EXECUTE_TASK_NORMAL" || records[0]["show_name"] != "Bob" || records[0]["date"] != "2026-08-05T18:28Z" {
		t.Fatalf("operation record = %#v", records[0])
	}

	if ccUserIDs, ok := output["cc_user_ids"].([]string); !ok || len(ccUserIDs) != 1 || ccUserIDs[0] != "carol" {
		t.Fatalf("cc_user_ids = %#v", output["cc_user_ids"])
	}
	if attached, ok := output["attached_instance_ids"].([]string); !ok || len(attached) != 1 {
		t.Fatalf("attached_instance_ids = %#v", output["attached_instance_ids"])
	}
}

func TestNodeReportsARunningInstanceWithoutAResult(t *testing.T) {
	workflowClient, tokenClient := testClients()
	result := sampleResult()
	result.Status = stringPtr("RUNNING")
	result.Result = nil
	result.FinishTime = nil
	workflowClient.response.Body.Result = result
	node := NewNodeForTest(workflowClient, tokenClient)

	output, err := node.Run(context.Background(), map[string]any{"instance_id": "inst-1"}, testInfo())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if output["status"] != "RUNNING" || output["result"] != "" || output["finish_time"] != "" {
		t.Fatalf("running instance = %#v", output)
	}
}

func TestNodeRejectsBadInput(t *testing.T) {
	for name, testCase := range map[string]struct {
		input   map[string]any
		info    workflow.RuntimeInfo
		message string
	}{
		"missing instance id": {
			input:   map[string]any{},
			info:    testInfo(),
			message: "instance_id is required",
		},
		"blank instance id": {
			input:   map[string]any{"instance_id": "   "},
			info:    testInfo(),
			message: "instance_id is required",
		},
		"missing app key": {
			input:   map[string]any{"instance_id": "inst-1"},
			info:    workflow.RuntimeInfo{Secrets: map[string]string{"app_secret": "secret-1"}},
			message: "app_key secret is required",
		},
		"missing app secret": {
			input:   map[string]any{"instance_id": "inst-1"},
			info:    workflow.RuntimeInfo{Secrets: map[string]string{"app_key": "key-1"}},
			message: "app_secret secret is required",
		},
	} {
		t.Run(name, func(t *testing.T) {
			workflowClient, tokenClient := testClients()
			node := NewNodeForTest(workflowClient, tokenClient)
			_, err := node.Run(context.Background(), testCase.input, testCase.info)
			if err == nil || !strings.Contains(err.Error(), testCase.message) {
				t.Fatalf("error = %v, want it to mention %q", err, testCase.message)
			}
			if workflowClient.calls != 0 {
				t.Fatalf("a rejected input still called DingTalk")
			}
		})
	}
}

func TestNodeInfoDocumentsBothLanguages(t *testing.T) {
	info := NewNodeForTest(nil, nil).Info()
	if info.Type != "dingtalk.approval.get" {
		t.Fatalf("type = %q", info.Type)
	}
	for _, language := range []string{"en-US", "zh-CN"} {
		if strings.TrimSpace(info.Documentation[language]) == "" {
			t.Fatalf("documentation for %s is empty", language)
		}
	}
}
