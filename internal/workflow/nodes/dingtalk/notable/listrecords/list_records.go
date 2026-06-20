package listrecords

import (
	"context"
	"errors"
	"fmt"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	notable "github.com/alibabacloud-go/dingtalk/notable_1_0"
	oauth2 "github.com/alibabacloud-go/dingtalk/oauth2_1_0"
	util "github.com/alibabacloud-go/tea-utils/v2/service"

	"autable/internal/workflow"
)

type dingTalkNotableListRecordsClient interface {
	ListRecordsWithOptions(baseID *string, sheetIDOrName *string, request *notable.ListRecordsRequest, headers *notable.ListRecordsHeaders, runtime *util.RuntimeOptions) (*notable.ListRecordsResponse, error)
}

type dingTalkAccessTokenClient interface {
	GetAccessToken(request *oauth2.GetAccessTokenRequest) (*oauth2.GetAccessTokenResponse, error)
}

type Node struct {
	notableClient     dingTalkNotableListRecordsClient
	accessTokenClient dingTalkAccessTokenClient
	clientErr         error
}

func NewNode() Node {
	config := &openapi.Config{
		Protocol: stringPtr("HTTPS"),
	}
	notableClient, err := notable.NewClient(config)
	if err != nil {
		return Node{clientErr: err}
	}
	accessTokenClient, err := oauth2.NewClient(config)
	return Node{
		notableClient:     notableClient,
		accessTokenClient: accessTokenClient,
		clientErr:         err,
	}
}

func NewNodeForTest(notableClient dingTalkNotableListRecordsClient, accessTokenClient dingTalkAccessTokenClient) Node {
	return Node{notableClient: notableClient, accessTokenClient: accessTokenClient}
}

func (node Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "dingtalk.notable.records.list",
		DisplayName:   "DingTalk AI table records",
		Description:   "Lists records from a DingTalk AI table through the DingTalk OpenAPI SDK.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "field_id_or_names", Type: "string[]", Description: "Optional field IDs or names to return."},
			{Name: "max_results", Type: "int", Description: "Optional page size."},
			{Name: "next_token", Type: "string", Description: "Optional pagination token."},
			{Name: "filter", Type: "object", Description: "Optional filter object with combination and conditions."},
		},
		Outputs: []workflow.Port{
			{Name: "records", Type: "DingTalkNotableRecord[]"},
			{Name: "has_more", Type: "boolean"},
			{Name: "next_token", Type: "string"},
			{Name: "status_code", Type: "int"},
		},
		Variables: []workflow.Port{
			{Name: "base_id", Type: "string", Description: "DingTalk AI table base ID."},
			{Name: "sheet_id_or_name", Type: "string", Description: "Sheet ID or sheet name."},
			{Name: "operator_id", Type: "string", Description: "Operator union ID."},
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
	if node.notableClient == nil {
		return nil, errors.New("dingtalk notable client is not configured")
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
	baseID := strings.TrimSpace(info.Variables["base_id"])
	if baseID == "" {
		return nil, errors.New("dingtalk base_id is required")
	}
	sheetIDOrName := strings.TrimSpace(info.Variables["sheet_id_or_name"])
	if sheetIDOrName == "" {
		return nil, errors.New("dingtalk sheet_id_or_name is required")
	}
	operatorID := strings.TrimSpace(info.Variables["operator_id"])
	if operatorID == "" {
		return nil, errors.New("dingtalk operator_id is required")
	}

	request, err := listRecordsRequest(input, operatorID)
	if err != nil {
		return nil, err
	}
	accessToken, err := node.accessToken(ctx, appKey, appSecret)
	if err != nil {
		return nil, err
	}
	response, err := node.notableClient.ListRecordsWithOptions(
		stringPtr(baseID),
		stringPtr(sheetIDOrName),
		request,
		(&notable.ListRecordsHeaders{}).SetXAcsDingtalkAccessToken(accessToken),
		&util.RuntimeOptions{},
	)
	output := listRecordsOutput(response)
	if err != nil {
		return output, err
	}
	return output, nil
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
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if response == nil || response.Body == nil {
		return "", errors.New("dingtalk access token response is empty")
	}
	accessToken := strings.TrimSpace(stringPtrValue(response.Body.AccessToken))
	if accessToken == "" {
		return "", errors.New("dingtalk access token response is empty")
	}
	return accessToken, nil
}

func listRecordsRequest(input map[string]any, operatorID string) (*notable.ListRecordsRequest, error) {
	request := (&notable.ListRecordsRequest{}).SetOperatorId(operatorID)
	if values := stringSliceInput(input, "field_id_or_names"); len(values) > 0 {
		request.SetFieldIdOrNames(stringPtrs(values))
	}
	if maxResults, ok := int32Input(input, "max_results"); ok {
		request.SetMaxResults(maxResults)
	}
	if nextToken := strings.TrimSpace(stringInput(input, "next_token")); nextToken != "" {
		request.SetNextToken(nextToken)
	}
	filter, err := listRecordsFilter(input["filter"])
	if err != nil {
		return nil, err
	}
	if filter != nil {
		request.SetFilter(filter)
	}
	return request, nil
}

func listRecordsFilter(value any) (*notable.ListRecordsRequestFilter, error) {
	if value == nil {
		return nil, nil
	}
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("dingtalk filter must be an object")
	}
	filter := &notable.ListRecordsRequestFilter{}
	if combination := strings.TrimSpace(stringValue(raw["combination"])); combination != "" {
		filter.SetCombination(combination)
	}
	rawConditions, ok := raw["conditions"].([]any)
	if !ok || len(rawConditions) == 0 {
		return nil, errors.New("dingtalk filter.conditions must be a non-empty array")
	}
	conditions := make([]*notable.ListRecordsRequestFilterConditions, 0, len(rawConditions))
	for index, rawCondition := range rawConditions {
		condition, ok := rawCondition.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("dingtalk filter.conditions[%d] must be an object", index)
		}
		field := strings.TrimSpace(stringValue(condition["field"]))
		operator := strings.TrimSpace(stringValue(condition["operator"]))
		if field == "" || operator == "" {
			return nil, fmt.Errorf("dingtalk filter.conditions[%d] field and operator are required", index)
		}
		parsed := (&notable.ListRecordsRequestFilterConditions{}).
			SetField(field).
			SetOperator(operator).
			SetValue(anySlice(condition["value"]))
		conditions = append(conditions, parsed)
	}
	filter.SetConditions(conditions)
	return filter, nil
}

func listRecordsOutput(response *notable.ListRecordsResponse) map[string]any {
	output := map[string]any{
		"records":    []map[string]any{},
		"has_more":   false,
		"next_token": "",
	}
	if response == nil {
		return output
	}
	if response.StatusCode != nil {
		output["status_code"] = int(*response.StatusCode)
	}
	if response.Body == nil {
		return output
	}
	if response.Body.HasMore != nil {
		output["has_more"] = *response.Body.HasMore
	}
	if response.Body.NextToken != nil {
		output["next_token"] = *response.Body.NextToken
	}
	records := make([]map[string]any, 0, len(response.Body.Records))
	for _, record := range response.Body.Records {
		if record == nil {
			continue
		}
		values := map[string]any{
			"id":     stringPtrValue(record.Id),
			"fields": cloneAnyMapFromInterface(record.Fields),
		}
		if record.CreatedTime != nil {
			values["created_time"] = *record.CreatedTime
		}
		if record.LastModifiedTime != nil {
			values["last_modified_time"] = *record.LastModifiedTime
		}
		if record.CreatedBy != nil && record.CreatedBy.UnionId != nil {
			values["created_by"] = map[string]any{"union_id": *record.CreatedBy.UnionId}
		}
		if record.LastModifiedBy != nil && record.LastModifiedBy.UnionId != nil {
			values["last_modified_by"] = map[string]any{"union_id": *record.LastModifiedBy.UnionId}
		}
		records = append(records, values)
	}
	output["records"] = records
	return output
}

func int32Input(input map[string]any, key string) (int32, bool) {
	switch value := input[key].(type) {
	case int:
		return int32(value), true
	case int32:
		return value, true
	case int64:
		return int32(value), true
	case float64:
		return int32(value), true
	default:
		return 0, false
	}
}

func stringInput(input map[string]any, key string) string {
	if value, ok := input[key].(string); ok {
		return value
	}
	return ""
}

func stringSliceInput(input map[string]any, key string) []string {
	value, ok := input[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func anySlice(value any) []interface{} {
	if value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []interface{}:
		return append([]interface{}(nil), typed...)
	default:
		return []interface{}{typed}
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func stringPtrs(values []string) []*string {
	pointers := make([]*string, 0, len(values))
	for _, value := range values {
		value := value
		pointers = append(pointers, &value)
	}
	return pointers
}

func stringPtr(value string) *string {
	return &value
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneAnyMapFromInterface(values map[string]interface{}) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

var _ workflow.Node = Node{}
