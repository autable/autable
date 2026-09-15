package comment

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
	request  *dingworkflow.AddProcessInstanceCommentRequest
	headers  *dingworkflow.AddProcessInstanceCommentHeaders
	response *dingworkflow.AddProcessInstanceCommentResponse
	err      error
	calls    int
}

func (client *fakeWorkflowClient) AddProcessInstanceCommentWithOptions(request *dingworkflow.AddProcessInstanceCommentRequest, headers *dingworkflow.AddProcessInstanceCommentHeaders, _ *util.RuntimeOptions) (*dingworkflow.AddProcessInstanceCommentResponse, error) {
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
		Variables: map[string]string{
			"comment_user_id": "operator",
		},
	}
}

func testClients() (*fakeWorkflowClient, *fakeAccessTokenClient) {
	workflowClient := &fakeWorkflowClient{
		response: &dingworkflow.AddProcessInstanceCommentResponse{
			Body: (&dingworkflow.AddProcessInstanceCommentResponseBody{}).SetResult(true).SetSuccess(true),
		},
	}
	tokenClient := &fakeAccessTokenClient{
		response: &oauth2.GetAccessTokenResponse{
			Body: (&oauth2.GetAccessTokenResponseBody{}).SetAccessToken("token-1"),
		},
	}
	return workflowClient, tokenClient
}

func TestNodePostsACommentAsTheConfiguredUser(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	output, err := node.Run(context.Background(), map[string]any{
		"instance_id": "  inst-1  ",
		"text":        " code: ABC-123 ",
	}, testInfo())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if tokenClient.appKey != "key-1" || tokenClient.appSecret != "secret-1" {
		t.Fatalf("unexpected credentials %q/%q", tokenClient.appKey, tokenClient.appSecret)
	}
	request := workflowClient.request
	if derefString(request.ProcessInstanceId) != "inst-1" || derefString(request.Text) != "code: ABC-123" {
		t.Fatalf("request = %#v", request)
	}
	if derefString(request.CommentUserId) != "operator" {
		t.Fatalf("comment user = %q, want the variable", derefString(request.CommentUserId))
	}
	if request.File != nil {
		t.Fatalf("no file should be attached: %#v", request.File)
	}
	if got := derefString(workflowClient.headers.XAcsDingtalkAccessToken); got != "token-1" {
		t.Fatalf("access token header = %q", got)
	}
	if workflowClient.calls != 1 {
		t.Fatalf("expected exactly one request, got %d", workflowClient.calls)
	}
	if output["instance_id"] != "inst-1" || output["comment_user_id"] != "operator" {
		t.Fatalf("output = %#v", output)
	}
}

func TestNodeLetsTheInputOverrideTheCommentUser(t *testing.T) {
	workflowClient, tokenClient := testClients()
	node := NewNodeForTest(workflowClient, tokenClient)

	output, err := node.Run(context.Background(), map[string]any{
		"instance_id":     "inst-1",
		"text":            "hello",
		"comment_user_id": " bob ",
	}, testInfo())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if derefString(workflowClient.request.CommentUserId) != "bob" || output["comment_user_id"] != "bob" {
		t.Fatalf("comment user = %q", derefString(workflowClient.request.CommentUserId))
	}
}

func TestNodeFailsWhenDingTalkRejectsTheComment(t *testing.T) {
	workflowClient, tokenClient := testClients()
	workflowClient.response.Body.SetResult(false)
	node := NewNodeForTest(workflowClient, tokenClient)

	_, err := node.Run(context.Background(), map[string]any{"instance_id": "inst-1", "text": "hello"}, testInfo())
	if err == nil || !strings.Contains(err.Error(), "did not accept the comment") {
		t.Fatalf("error = %v", err)
	}
}

func TestNodeRejectsBadInput(t *testing.T) {
	for name, testCase := range map[string]struct {
		input   map[string]any
		info    workflow.RuntimeInfo
		message string
	}{
		"missing instance id": {
			input:   map[string]any{"text": "hello"},
			info:    testInfo(),
			message: "instance_id is required",
		},
		"missing text": {
			input:   map[string]any{"instance_id": "inst-1", "text": "   "},
			info:    testInfo(),
			message: "text is required",
		},
		"missing comment user": {
			input:   map[string]any{"instance_id": "inst-1", "text": "hello"},
			info:    workflow.RuntimeInfo{Secrets: testInfo().Secrets},
			message: "comment_user_id is required",
		},
		"missing app key": {
			input:   map[string]any{"instance_id": "inst-1", "text": "hello"},
			info:    workflow.RuntimeInfo{Secrets: map[string]string{"app_secret": "secret-1"}, Variables: testInfo().Variables},
			message: "app_key secret is required",
		},
		"missing app secret": {
			input:   map[string]any{"instance_id": "inst-1", "text": "hello"},
			info:    workflow.RuntimeInfo{Secrets: map[string]string{"app_key": "key-1"}, Variables: testInfo().Variables},
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
	if info.Type != "dingtalk.approval.comment" {
		t.Fatalf("type = %q", info.Type)
	}
	for _, language := range []string{"en-US", "zh-CN"} {
		if strings.TrimSpace(info.Documentation[language]) == "" {
			t.Fatalf("documentation for %s is empty", language)
		}
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
