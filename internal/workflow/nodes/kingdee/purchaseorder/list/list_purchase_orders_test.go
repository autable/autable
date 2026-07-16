package list

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"autable/internal/kingdee"
	"autable/internal/workflow"
)

type fakeBillQueryClient struct {
	pages    [][][]any
	requests []map[string]any
}

func (client *fakeBillQueryClient) ExecuteBillQuery(_ context.Context, data map[string]any) ([][]any, error) {
	client.requests = append(client.requests, data)
	if len(client.requests) > len(client.pages) {
		return [][]any{}, nil
	}
	return client.pages[len(client.requests)-1], nil
}

func testRuntimeInfo() workflow.RuntimeInfo {
	return workflow.RuntimeInfo{
		Variables: map[string]string{
			"server_url": "https://erp.example.com/K3Cloud",
			"acct_id":    "dcid-1",
			"user_name":  "user-1",
			"app_id":     "123456_secret",
			"org_num":    "100",
		},
		Secrets: map[string]string{"app_secret": "s3cret"},
	}
}

func testNode(client *fakeBillQueryClient, capture *kingdee.Config) Node {
	return NewNodeForTest(func(config kingdee.Config) (billQueryClient, error) {
		if capture != nil {
			*capture = config
		}
		return client, nil
	})
}

func fullRow(id float64, billNo string) []any {
	return []any{id, billNo, "2026-07-16", "C", "SUP001", "MAT001", "steel", 12.0, 100.0, 113.0, 13.0, 1200.0, 1356.0}
}

func TestNodePagesThroughAllPurchaseOrders(t *testing.T) {
	firstPage := make([][]any, 0, 2000)
	for i := range 2000 {
		firstPage = append(firstPage, fullRow(float64(i), "CGDD0001"))
	}
	client := &fakeBillQueryClient{pages: [][][]any{firstPage, {fullRow(2000, "CGDD9999")}}}
	var config kingdee.Config
	node := testNode(client, &config)

	output, err := node.Run(context.Background(), map[string]any{}, testRuntimeInfo())
	if err != nil {
		t.Fatal(err)
	}
	if output["count"] != 2001 {
		t.Fatalf("unexpected count: %#v", output["count"])
	}
	records := output["records"].([]map[string]any)
	if records[0]["bill_no"] != "CGDD0001" || records[0]["qty"] != 12.0 || records[0]["all_amount"] != 1356.0 {
		t.Fatalf("unexpected first record: %#v", records[0])
	}
	if records[2000]["bill_no"] != "CGDD9999" {
		t.Fatalf("unexpected last record: %#v", records[2000])
	}

	if len(client.requests) != 2 {
		t.Fatalf("expected two pages, got %d requests", len(client.requests))
	}
	first := client.requests[0]
	if first["FormId"] != "PUR_PurchaseOrder" || first["FilterString"] != "" || first["StartRow"] != 0 {
		t.Fatalf("unexpected first request: %#v", first)
	}
	if !strings.HasPrefix(first["FieldKeys"].(string), "FID,FBillNo,FDate,") {
		t.Fatalf("unexpected field keys: %#v", first["FieldKeys"])
	}
	if client.requests[1]["StartRow"] != 2000 {
		t.Fatalf("unexpected second page start: %#v", client.requests[1])
	}

	if config.ServerURL != "https://erp.example.com/K3Cloud" || config.OrgNum != 100 || config.AppSecret != "s3cret" {
		t.Fatalf("unexpected client config: %#v", config)
	}
	if config.SkipTLSVerify {
		t.Fatal("expected TLS verification by default")
	}
}

func TestNodeSupportsCustomFilterFieldsAndLimit(t *testing.T) {
	client := &fakeBillQueryClient{pages: [][][]any{{{"CGDD0001", 5.0}}}}
	node := testNode(client, nil)

	output, err := node.Run(context.Background(), map[string]any{
		"filter_string": "FDate>='2026-01-01'",
		"field_keys":    []any{"FBillNo", "FQty"},
		"limit":         500,
	}, testRuntimeInfo())
	if err != nil {
		t.Fatal(err)
	}
	records := output["records"].([]map[string]any)
	if len(records) != 1 || records[0]["FBillNo"] != "CGDD0001" || records[0]["FQty"] != 5.0 {
		t.Fatalf("unexpected records: %#v", records)
	}
	request := client.requests[0]
	if request["FilterString"] != "FDate>='2026-01-01'" || request["FieldKeys"] != "FBillNo,FQty" || request["Limit"] != 500 {
		t.Fatalf("unexpected request: %#v", request)
	}
}

func TestNodeRejectsColumnCountMismatch(t *testing.T) {
	client := &fakeBillQueryClient{pages: [][][]any{{{"only-one-value"}}}}
	node := testNode(client, nil)

	_, err := node.Run(context.Background(), map[string]any{}, testRuntimeInfo())
	if err == nil || !strings.Contains(err.Error(), "returned 1 values for 13 fields") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestNodeValidatesConfigurationAndInputs(t *testing.T) {
	node := testNode(&fakeBillQueryClient{}, nil)

	cases := []struct {
		mutate  func(info *workflow.RuntimeInfo, input map[string]any)
		message string
	}{
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Variables, "server_url") }, "server_url"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Variables, "acct_id") }, "acct_id"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Variables, "user_name") }, "user_name"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Variables, "app_id") }, "app_id"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { delete(info.Secrets, "app_secret") }, "app_secret"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { info.Variables["org_num"] = "abc" }, "org_num"},
		{func(info *workflow.RuntimeInfo, _ map[string]any) { info.Variables["skip_tls_verify"] = "maybe" }, "skip_tls_verify"},
		{func(_ *workflow.RuntimeInfo, input map[string]any) { input["limit"] = 5000 }, "between 1 and 2000"},
		{func(_ *workflow.RuntimeInfo, input map[string]any) { input["limit"] = "many" }, "must be a number"},
		{func(_ *workflow.RuntimeInfo, input map[string]any) { input["field_keys"] = []any{" "} }, "must not be empty"},
	}
	for _, testCase := range cases {
		info := testRuntimeInfo()
		input := map[string]any{}
		testCase.mutate(&info, input)
		_, err := node.Run(context.Background(), input, info)
		if err == nil || !strings.Contains(err.Error(), testCase.message) {
			t.Fatalf("expected error containing %q, got %v", testCase.message, err)
		}
	}
}

func TestNodeQueriesRealClientAgainstFakeGateway(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Kd-Appkey") != "123456_YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4" {
			t.Errorf("unexpected app key header: %q", r.Header.Get("X-Kd-Appkey"))
		}
		w.Write([]byte(`[[1, "CGDD0001", "2026-07-16", "C", "SUP001", "MAT001", "steel", 12.0, 100.0, 113.0, 13.0, 1200.0, 1356.0]]`))
	}))
	defer server.Close()

	info := testRuntimeInfo()
	info.Variables["server_url"] = server.URL + "/K3Cloud"
	info.Variables["app_id"] = "123456_YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4"

	output, err := NewNode().Run(context.Background(), map[string]any{}, info)
	if err != nil {
		t.Fatal(err)
	}
	if output["count"] != 1 {
		t.Fatalf("unexpected count: %#v", output["count"])
	}
	records := output["records"].([]map[string]any)
	if records[0]["bill_no"] != "CGDD0001" || records[0]["material_name"] != "steel" {
		t.Fatalf("unexpected record: %#v", records[0])
	}
}
