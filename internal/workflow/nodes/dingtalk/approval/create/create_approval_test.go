package create

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
	request  *dingworkflow.StartProcessInstanceRequest
	headers  *dingworkflow.StartProcessInstanceHeaders
	response *dingworkflow.StartProcessInstanceResponse
	err      error
}

func (client *fakeWorkflowClient) StartProcessInstanceWithOptions(request *dingworkflow.StartProcessInstanceRequest, headers *dingworkflow.StartProcessInstanceHeaders, _ *util.RuntimeOptions) (*dingworkflow.StartProcessInstanceResponse, error) {
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
		Variables: map[string]string{
			"process_code":       "PROC-TEST-1",
			"originator_user_id": "manager001",
			"dept_id":            "-1",
		},
		Secrets: map[string]string{
			"app_key":    "key-1",
			"app_secret": "secret-1",
		},
	}
}

func testClients() (*fakeWorkflowClient, *fakeAccessTokenClient) {
	workflowClient := &fakeWorkflowClient{
		response: &dingworkflow.StartProcessInstanceResponse{
			Body: (&dingworkflow.StartProcessInstanceResponseBody{}).SetInstanceId("inst-42"),
		},
	}
	tokenClient := &fakeAccessTokenClient{
		response: &oauth2.GetAccessTokenResponse{
			Body: (&oauth2.GetAccessTokenResponseBody{}).SetAccessToken("token-1"),
		},
	}
	return workflowClient, tokenClient
}

func TestNodeStartsApprovalInstance(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	output, err := node.Run(context.Background(), map[string]any{
		"form_values": []any{
			map[string]any{"name": "单号", "value": "CGDD0001"},
			map[string]any{"name": "金额", "value": 1356.5},
			map[string]any{"name": "明细", "value": []any{"a", "b"}},
		},
	}, testInfo())
	if err != nil {
		t.Fatal(err)
	}
	if output["instance_id"] != "inst-42" {
		t.Fatalf("unexpected output: %#v", output)
	}

	if tokenClient.appKey != "key-1" || tokenClient.appSecret != "secret-1" {
		t.Fatalf("unexpected token request: %#v", tokenClient)
	}
	if workflowClient.headers == nil || workflowClient.headers.XAcsDingtalkAccessToken == nil || *workflowClient.headers.XAcsDingtalkAccessToken != "token-1" {
		t.Fatalf("expected access token header, got %#v", workflowClient.headers)
	}
	request := workflowClient.request
	if request == nil || *request.ProcessCode != "PROC-TEST-1" || *request.OriginatorUserId != "manager001" {
		t.Fatalf("unexpected request: %#v", request)
	}
	if request.DeptId == nil || *request.DeptId != -1 {
		t.Fatalf("expected dept id -1 from the variable, got %#v", request.DeptId)
	}
	if len(request.FormComponentValues) != 3 {
		t.Fatalf("expected three form values, got %#v", request.FormComponentValues)
	}
	if *request.FormComponentValues[0].Name != "单号" || *request.FormComponentValues[0].Value != "CGDD0001" {
		t.Fatalf("unexpected first form value: %#v", request.FormComponentValues[0])
	}
	if *request.FormComponentValues[1].Value != "1356.5" {
		t.Fatalf("expected number stringified, got %q", *request.FormComponentValues[1].Value)
	}
	if *request.FormComponentValues[2].Value != `["a","b"]` {
		t.Fatalf("expected array JSON-encoded, got %q", *request.FormComponentValues[2].Value)
	}
}

func TestNodeInputOverridesOriginatorAndDept(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	if _, err := node.Run(context.Background(), map[string]any{
		"originator_user_id": "employee007",
		"dept_id":            42,
		"form_values":        []any{map[string]any{"name": "单号", "value": "X"}},
	}, testInfo()); err != nil {
		t.Fatal(err)
	}
	if *workflowClient.request.OriginatorUserId != "employee007" {
		t.Fatalf("expected input originator to win, got %q", *workflowClient.request.OriginatorUserId)
	}
	if *workflowClient.request.DeptId != 42 {
		t.Fatalf("expected input dept id to win, got %d", *workflowClient.request.DeptId)
	}
}

func TestNodeValidatesConfigurationAndInputs(t *testing.T) {
	cases := []struct {
		mutate  func(info *workflow.RuntimeInfo, input map[string]any)
		message string
	}{
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Secrets, "app_key") }, "app_key"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Secrets, "app_secret") }, "app_secret"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Variables, "process_code") }, "process_code"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Variables, "originator_user_id") }, "originator_user_id"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { info.Variables["dept_id"] = "abc" }, "dept_id"},
		{func(_ *workflow.RuntimeInfo, input map[string]any) { delete(input, "form_values") }, "form_values"},
		{func(_ *workflow.RuntimeInfo, input map[string]any) { input["form_values"] = []any{} }, "form_values"},
		{func(_ *workflow.RuntimeInfo, input map[string]any) {
			input["form_values"] = []any{map[string]any{"value": "x"}}
		}, "name is required"},
		{func(_ *workflow.RuntimeInfo, input map[string]any) {
			input["form_values"] = []any{map[string]any{"name": "单号"}}
		}, "value is required"},
		{func(_ *workflow.RuntimeInfo, input map[string]any) { input["dept_id"] = "many" }, "must be a number"},
	}
	for _, testCase := range cases {
		workflowClient, tokenClient := testClients()
		node := NewNodeForTest(workflowClient, tokenClient)
		info := testInfo()
		input := map[string]any{"form_values": []any{map[string]any{"name": "单号", "value": "X"}}}
		testCase.mutate(&info, input)
		_, err := node.Run(context.Background(), input, info)
		if err == nil || !strings.Contains(err.Error(), testCase.message) {
			t.Fatalf("expected error containing %q, got %v", testCase.message, err)
		}
	}
}

func TestNodeRejectsEmptyInstanceID(t *testing.T) {
	workflowClient, tokenClient := testClients()
	workflowClient.response = &dingworkflow.StartProcessInstanceResponse{Body: &dingworkflow.StartProcessInstanceResponseBody{}}
	node := NewNodeForTest(workflowClient, tokenClient)

	_, err := node.Run(context.Background(), map[string]any{
		"form_values": []any{map[string]any{"name": "单号", "value": "X"}},
	}, testInfo())
	if err == nil || !strings.Contains(err.Error(), "no instance id") {
		t.Fatalf("expected missing instance id error, got %v", err)
	}
}

func TestNodeSendsExtValueForComponentsThatNeedIt(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	if _, err := node.Run(context.Background(), map[string]any{
		"form_values": []any{
			// A link to another approval shows a title but identifies its
			// target only through ext_value.
			map[string]any{
				"name":      "Linked approval",
				"value":     []any{"Purchase contract"},
				"ext_value": map[string]any{"list": []any{map[string]any{"procInstId": "inst-7"}}},
			},
			map[string]any{"name": "Amount", "value": 42},
		},
	}, testInfo()); err != nil {
		t.Fatalf("run: %v", err)
	}

	components := workflowClient.request.FormComponentValues
	if len(components) != 2 {
		t.Fatalf("components = %d", len(components))
	}
	if got := derefString(components[0].Value); got != `["Purchase contract"]` {
		t.Fatalf("value = %s", got)
	}
	if got := derefString(components[0].ExtValue); got != `{"list":[{"procInstId":"inst-7"}]}` {
		t.Fatalf("ext_value = %s", got)
	}
	// A component that did not ask for one must not gain an empty ext_value,
	// which DingTalk reads as a cleared field rather than an absent one.
	if components[1].ExtValue != nil {
		t.Fatalf("ext_value leaked onto a plain component: %q", *components[1].ExtValue)
	}
}

func TestNodeRejectsAnUnserializableExtValue(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	_, err := node.Run(context.Background(), map[string]any{
		"form_values": []any{
			map[string]any{"name": "Linked approval", "value": "x", "ext_value": make(chan int)},
		},
	}, testInfo())
	if err == nil || !strings.Contains(err.Error(), "ext_value") {
		t.Fatalf("error = %v, want it to mention ext_value", err)
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
