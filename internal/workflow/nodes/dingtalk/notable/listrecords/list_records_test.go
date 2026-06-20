package listrecords

import (
	"context"
	"testing"

	notable "github.com/alibabacloud-go/dingtalk/notable_1_0"
	oauth2 "github.com/alibabacloud-go/dingtalk/oauth2_1_0"
	util "github.com/alibabacloud-go/tea-utils/v2/service"

	"autable/internal/workflow"
)

type fakeNotableListRecordsClient struct {
	baseID        string
	sheetIDOrName string
	request       *notable.ListRecordsRequest
	headers       *notable.ListRecordsHeaders
	response      *notable.ListRecordsResponse
	err           error
}

func (client *fakeNotableListRecordsClient) ListRecordsWithOptions(baseID *string, sheetIDOrName *string, request *notable.ListRecordsRequest, headers *notable.ListRecordsHeaders, _ *util.RuntimeOptions) (*notable.ListRecordsResponse, error) {
	client.baseID = stringPtrValue(baseID)
	client.sheetIDOrName = stringPtrValue(sheetIDOrName)
	client.request = request
	client.headers = headers
	return client.response, client.err
}

type fakeDingTalkAccessTokenClient struct {
	appKey    string
	appSecret string
	response  *oauth2.GetAccessTokenResponse
	err       error
}

func (client *fakeDingTalkAccessTokenClient) GetAccessToken(request *oauth2.GetAccessTokenRequest) (*oauth2.GetAccessTokenResponse, error) {
	if request != nil {
		client.appKey = stringPtrValue(request.AppKey)
		client.appSecret = stringPtrValue(request.AppSecret)
	}
	return client.response, client.err
}

func TestDingTalkNotableListRecordsNodeCallsSDK(t *testing.T) {
	notableClient := &fakeNotableListRecordsClient{
		response: &notable.ListRecordsResponse{
			StatusCode: int32Ptr(200),
			Body: (&notable.ListRecordsResponseBody{}).
				SetHasMore(true).
				SetNextToken("next-page").
				SetRecords([]*notable.ListRecordsResponseBodyRecords{
					{
						Id:               stringPtr("rec-1"),
						Fields:           map[string]interface{}{"Name": "Ada", "Score": float64(9)},
						CreatedTime:      int64Ptr(123),
						LastModifiedTime: int64Ptr(456),
						CreatedBy:        (&notable.ListRecordsResponseBodyRecordsCreatedBy{}).SetUnionId("creator-union"),
						LastModifiedBy:   (&notable.ListRecordsResponseBodyRecordsLastModifiedBy{}).SetUnionId("editor-union"),
					},
				}),
		},
	}
	accessTokenClient := &fakeDingTalkAccessTokenClient{
		response: (&oauth2.GetAccessTokenResponse{}).
			SetBody((&oauth2.GetAccessTokenResponseBody{}).SetAccessToken("dingtalk-token").SetExpireIn(7200)),
	}
	node := NewNodeForTest(notableClient, accessTokenClient)
	output, err := node.Run(context.Background(), map[string]any{
		"field_id_or_names": []any{
			"Name",
			"Score",
		},
		"max_results": float64(20),
		"next_token":  "page-1",
		"filter": map[string]any{
			"combination": "AND",
			"conditions": []any{
				map[string]any{
					"field":    "Status",
					"operator": "Equal",
					"value":    []any{"Active"},
				},
			},
		},
	}, workflow.RuntimeInfo{
		Variables: map[string]string{
			"base_id":          "base-1",
			"sheet_id_or_name": "Contacts",
			"operator_id":      "operator-union",
		},
		Secrets: map[string]string{"app_key": "app-key", "app_secret": "app-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if accessTokenClient.appKey != "app-key" || accessTokenClient.appSecret != "app-secret" {
		t.Fatalf("unexpected app credentials: app_key=%q app_secret=%q", accessTokenClient.appKey, accessTokenClient.appSecret)
	}
	if notableClient.baseID != "base-1" || notableClient.sheetIDOrName != "Contacts" {
		t.Fatalf("unexpected path params: base=%q sheet=%q", notableClient.baseID, notableClient.sheetIDOrName)
	}
	if notableClient.headers == nil || stringPtrValue(notableClient.headers.XAcsDingtalkAccessToken) != "dingtalk-token" {
		t.Fatalf("expected access token header, got %#v", notableClient.headers)
	}
	if notableClient.request == nil || stringPtrValue(notableClient.request.OperatorId) != "operator-union" {
		t.Fatalf("expected operator request, got %#v", notableClient.request)
	}
	if len(notableClient.request.FieldIdOrNames) != 2 || stringPtrValue(notableClient.request.FieldIdOrNames[0]) != "Name" {
		t.Fatalf("unexpected field selection: %#v", notableClient.request.FieldIdOrNames)
	}
	if notableClient.request.MaxResults == nil || *notableClient.request.MaxResults != 20 {
		t.Fatalf("unexpected max results: %#v", notableClient.request.MaxResults)
	}
	if stringPtrValue(notableClient.request.NextToken) != "page-1" {
		t.Fatalf("unexpected next token: %#v", notableClient.request.NextToken)
	}
	if notableClient.request.Filter == nil || stringPtrValue(notableClient.request.Filter.Combination) != "AND" {
		t.Fatalf("unexpected filter: %#v", notableClient.request.Filter)
	}
	if len(notableClient.request.Filter.Conditions) != 1 || stringPtrValue(notableClient.request.Filter.Conditions[0].Field) != "Status" {
		t.Fatalf("unexpected filter conditions: %#v", notableClient.request.Filter.Conditions)
	}

	if output["status_code"] != 200 || output["has_more"] != true || output["next_token"] != "next-page" {
		t.Fatalf("unexpected output metadata: %#v", output)
	}
	records, ok := output["records"].([]map[string]any)
	if !ok || len(records) != 1 {
		t.Fatalf("unexpected records output: %#v", output["records"])
	}
	if records[0]["id"] != "rec-1" || records[0]["created_time"] != int64(123) || records[0]["last_modified_time"] != int64(456) {
		t.Fatalf("unexpected record metadata: %#v", records[0])
	}
	fields, ok := records[0]["fields"].(map[string]any)
	if !ok || fields["Name"] != "Ada" {
		t.Fatalf("unexpected record fields: %#v", records[0]["fields"])
	}
}

func TestDingTalkNotableListRecordsNodeRequiresInputs(t *testing.T) {
	node := NewNodeForTest(&fakeNotableListRecordsClient{}, &fakeDingTalkAccessTokenClient{})
	if _, err := node.Run(context.Background(), map[string]any{}, workflow.RuntimeInfo{}); err == nil {
		t.Fatal("expected missing app_key error")
	}
	if _, err := node.Run(context.Background(), map[string]any{}, workflow.RuntimeInfo{Secrets: map[string]string{"app_key": "key"}}); err == nil {
		t.Fatal("expected missing app_secret error")
	}
	if _, err := node.Run(context.Background(), map[string]any{}, workflow.RuntimeInfo{Secrets: map[string]string{"app_key": "key", "app_secret": "secret"}}); err == nil {
		t.Fatal("expected missing base_id error")
	}
	if _, err := node.Run(context.Background(), map[string]any{}, workflow.RuntimeInfo{
		Variables: map[string]string{"base_id": "base"},
		Secrets:   map[string]string{"app_key": "key", "app_secret": "secret"},
	}); err == nil {
		t.Fatal("expected missing sheet_id_or_name error")
	}
	if _, err := node.Run(context.Background(), map[string]any{}, workflow.RuntimeInfo{
		Variables: map[string]string{"base_id": "base", "sheet_id_or_name": "sheet"},
		Secrets:   map[string]string{"app_key": "key", "app_secret": "secret"},
	}); err == nil {
		t.Fatal("expected missing operator_id error")
	}
}

func TestDingTalkNotableListRecordsNodeIsAvailableInNodeInfos(t *testing.T) {
	runner := workflow.NewRunner(nil, NewNodeForTest(&fakeNotableListRecordsClient{}, &fakeDingTalkAccessTokenClient{}))
	infos := runner.NodeInfos()
	if len(infos) != 1 || infos[0].Type != "dingtalk.notable.records.list" {
		t.Fatalf("expected dingtalk notable node info, got %#v", infos)
	}
	if len(infos[0].Inputs) != 4 || infos[0].Inputs[0].Name != "field_id_or_names" {
		t.Fatalf("expected runtime input metadata, got %#v", infos[0].Inputs)
	}
	if len(infos[0].Variables) != 3 || infos[0].Variables[0].Name != "base_id" || infos[0].Variables[1].Name != "sheet_id_or_name" || infos[0].Variables[2].Name != "operator_id" {
		t.Fatalf("expected table config variable metadata, got %#v", infos[0].Variables)
	}
	if len(infos[0].Secrets) != 2 || infos[0].Secrets[0].Name != "app_key" || infos[0].Secrets[1].Name != "app_secret" {
		t.Fatalf("expected app credential secret metadata, got %#v", infos[0].Secrets)
	}
	if infos[0].Documentation["en-US"] == "" || infos[0].Documentation["zh-CN"] == "" {
		t.Fatalf("expected embedded documentation, got %#v", infos[0].Documentation)
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}
