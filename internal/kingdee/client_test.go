package kingdee

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Golden values generated with the official Python SDK
// (kingdee.cdp.webapi.sdk 8.2.0) to prove byte-level signing parity.

func TestDecodeAppSecretMatchesPythonSDK(t *testing.T) {
	decoded := decodeAppSecret("YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4")
	if decoded != "UVJWUANVXl8KXFlfWV1YSBNCShcUQRNL" {
		t.Fatalf("unexpected decoded secret: %q", decoded)
	}
	if decodeAppSecret("too-short") != "" {
		t.Fatal("expected invalid length to decode to empty string")
	}
	if decodeAppSecret(strings.Repeat("!", 32)) != "" {
		t.Fatal("expected invalid base64 to decode to empty string")
	}
}

func TestSignHMACMatchesPythonSDK(t *testing.T) {
	signature := signHMAC("POST\n%2Ftest\n\nx-api-nonce:1\nx-api-timestamp:1\n", "key123")
	if signature != "MThmZGFiM2U5MGZhNmU0MDQ0MWI1NTNkNmNmMjY2MDY0YzEzZTllNTA0OTBmM2M0NDI2NzFiMTI3NTFkZmU1ZQ==" {
		t.Fatalf("unexpected signature: %q", signature)
	}
}

func TestQuotePathMatchesPythonSDK(t *testing.T) {
	quoted := quotePath("/K3Cloud/Kingdee.BOS.WebApi.ServicesStub.DynamicFormService.ExecuteBillQuery.common.kdsvc")
	if quoted != "%2FK3Cloud%2FKingdee.BOS.WebApi.ServicesStub.DynamicFormService.ExecuteBillQuery.common.kdsvc" {
		t.Fatalf("unexpected quoted path: %q", quoted)
	}
}

func TestHeadersMatchPythonSDK(t *testing.T) {
	client, err := New(Config{
		ServerURL: "https://erp.example.com/K3Cloud",
		AcctID:    "dcid-1",
		UserName:  "user-1",
		AppID:     "123456_YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4",
		AppSecret: "test-app-secret-value",
		OrgNum:    100,
	})
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return time.Unix(1700000000, 0) }

	request, err := http.NewRequest(http.MethodPost, "https://erp.example.com/K3Cloud/"+billQueryService+".common.kdsvc", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.setHeaders(request, request.URL.String())

	expected := map[string]string{
		"X-Api-ClientID":     "123456",
		"X-Api-Auth-Version": "2.0",
		"x-api-timestamp":    "1700000000",
		"x-api-nonce":        "1700000000",
		"x-api-signheaders":  "x-api-timestamp,x-api-nonce",
		"X-Api-Signature":    "MjNhMzg5MmZiMWE1MTQ1ZGE5NWFkYmZiZWYxYzhlY2NjNzcwM2ZjNDJlZTczMzBhNzkxOTk1ODhjNDZkMTEzYw==",
		"X-Kd-Appkey":        "123456_YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4",
		"X-Kd-Appdata":       "ZGNpZC0xLHVzZXItMSwyMDUyLDEwMA==",
		"X-Kd-Signature":     "NjI5MzRiZGVhNGJhOWY2N2Y2YzQ2MzQ5ODA3N2JiZTRiMDRmOTUwNjI3YWIzODU1ODBkZDdhNzgwMzdmMDgxMg==",
	}
	for name, value := range expected {
		if actual := request.Header.Get(name); actual != value {
			t.Fatalf("header %s = %q, expected %q", name, actual, value)
		}
	}
}

func TestExecuteBillQueryAgainstFakeGateway(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, billQueryService+".common.kdsvc") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Kd-Appkey") == "" || r.Header.Get("X-Api-Signature") == "" {
			t.Error("expected signing headers")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		requests = append(requests, payload)
		http.SetCookie(w, &http.Cookie{Name: "kdservice-sessionid", Value: "sid-1"})
		if len(requests) == 1 {
			w.Write([]byte(`[[1, "CGDD0001", 5.0], [2, "CGDD0002", 7.5]]`))
			return
		}
		if r.Header.Get("kdservice-sessionid") != "sid-1" {
			t.Errorf("expected session to be replayed, got %q", r.Header.Get("kdservice-sessionid"))
		}
		if !strings.HasPrefix(r.Header.Get("Cookie"), "Theme=standard;") {
			t.Errorf("unexpected cookie header: %q", r.Header.Get("Cookie"))
		}
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, err := New(Config{
		ServerURL: server.URL + "/K3Cloud",
		AcctID:    "dcid-1",
		UserName:  "user-1",
		AppID:     "123456_YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4",
		AppSecret: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.ExecuteBillQuery(context.Background(), map[string]any{"FormId": "PUR_PurchaseOrder", "StartRow": 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0][1] != "CGDD0001" || rows[1][2] != 7.5 {
		t.Fatalf("unexpected rows: %#v", rows)
	}
	if rows2, err := client.ExecuteBillQuery(context.Background(), map[string]any{"StartRow": 2000}); err != nil || len(rows2) != 0 {
		t.Fatalf("unexpected second page: %#v err=%v", rows2, err)
	}
	if data, ok := requests[0]["data"].(map[string]any); !ok || data["FormId"] != "PUR_PurchaseOrder" {
		t.Fatalf("expected payload under data key, got %#v", requests[0])
	}
}

func TestExecuteSurfacesGatewayErrors(t *testing.T) {
	responses := []struct {
		status int
		body   string
	}{
		{http.StatusOK, "response_error: bill query is not licensed"},
		{http.StatusInternalServerError, "gateway exploded"},
		{http.StatusOK, `{"Result":{"ResponseStatus":{"IsSuccess":false}}}`},
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := responses[index]
		index++
		w.WriteHeader(response.status)
		w.Write([]byte(response.body))
	}))
	defer server.Close()

	client, err := New(Config{
		ServerURL: server.URL,
		AcctID:    "d", UserName: "u", AppID: "a_b", AppSecret: "s",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ExecuteBillQuery(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "bill query is not licensed") {
		t.Fatalf("expected response_error to surface, got %v", err)
	}
	if _, err := client.ExecuteBillQuery(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("expected status error, got %v", err)
	}
	if _, err := client.ExecuteBillQuery(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "unexpected shape") {
		t.Fatalf("expected shape error, got %v", err)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected missing server url to be rejected")
	}
	if _, err := New(Config{ServerURL: "https://erp.example.com/K3Cloud"}); err == nil {
		t.Fatal("expected missing credentials to be rejected")
	}
}
