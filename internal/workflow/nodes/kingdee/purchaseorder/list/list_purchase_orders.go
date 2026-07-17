package list

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"autable/internal/kingdee"
	"autable/internal/workflow"
)

// defaultFields maps Kingdee purchase order fields to output column names.
var defaultFields = []struct {
	Field  string
	Column string
}{
	{"FID", "id"},
	{"FBillNo", "bill_no"},
	{"FDate", "bill_date"},
	{"FDocumentStatus", "document_status"},
	{"FSupplierId.FNumber", "supplier_number"},
	{"FMaterialId.FNumber", "material_number"},
	{"FMaterialName", "material_name"},
	{"FQty", "qty"},
	{"FPrice", "price"},
	{"FTaxPrice", "tax_price"},
	{"FEntryTaxRate", "tax_rate"},
	{"FAmount", "amount"},
	{"FAllAmount", "all_amount"},
}

// maxPageSize is the Kingdee bill query row limit per request.
const maxPageSize = 2000

type billQueryClient interface {
	ExecuteBillQuery(ctx context.Context, data map[string]any) ([][]any, error)
}

type Node struct {
	newClient func(config kingdee.Config) (billQueryClient, error)
}

func NewNode() Node {
	return Node{newClient: func(config kingdee.Config) (billQueryClient, error) {
		return kingdee.New(config)
	}}
}

func NewNodeForTest(newClient func(config kingdee.Config) (billQueryClient, error)) Node {
	return Node{newClient: newClient}
}

func (node Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "kingdee.purchaseorder.list",
		DisplayName:   "Kingdee purchase orders",
		Description:   "Lists purchase order lines from Kingdee K3Cloud through the bill query WebAPI, paging until all rows are fetched.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "filter_string", Type: "string", Description: "Optional Kingdee filter expression, e.g. FDocumentStatus='C'; empty fetches all rows."},
			{Name: "field_keys", Type: "string[]", Description: "Optional Kingdee field keys to fetch instead of the built-in purchase order columns."},
			{Name: "limit", Type: "int", Description: "Optional page size per request, defaults to the Kingdee maximum of 2000."},
		},
		Outputs: []workflow.Port{
			{Name: "records", Type: "object[]", Description: "Purchase order lines keyed by column name."},
			{Name: "count", Type: "int"},
		},
		Variables: []workflow.Port{
			{Name: "server_url", Type: "string", Description: "K3Cloud gateway URL, e.g. https://erp.example.com/K3Cloud."},
			{Name: "acct_id", Type: "string", Description: "Account book ID."},
			{Name: "user_name", Type: "string", Description: "Integration user name."},
			{Name: "app_id", Type: "string", Description: "Third-party app ID."},
			{Name: "org_num", Type: "string", Description: "Optional organization number for multi-organization deployments."},
			{Name: "skip_tls_verify", Type: "string", Description: "Set to true to accept self-signed gateway certificates."},
		},
		Secrets: []workflow.Port{
			{Name: "app_secret", Type: "string", Description: "Third-party app secret."},
		},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	config, err := clientConfig(info)
	if err != nil {
		return nil, err
	}
	client, err := node.newClient(config)
	if err != nil {
		return nil, err
	}

	fields, columns, err := queryFields(input)
	if err != nil {
		return nil, err
	}
	filter := strings.TrimSpace(stringInput(input, "filter_string"))
	limit, err := pageLimit(input)
	if err != nil {
		return nil, err
	}

	logger := slog.Default().With("node", info.NodeType, "run_id", info.RunID, "instance", info.InstanceID)
	records := make([]map[string]any, 0)
	for startRow := 0; ; startRow += limit {
		pageStarted := time.Now()
		rows, err := client.ExecuteBillQuery(ctx, map[string]any{
			"FormId":       "PUR_PurchaseOrder",
			"FieldKeys":    strings.Join(fields, ","),
			"FilterString": filter,
			"OrderString":  "FID ASC",
			"TopRowCount":  0,
			"StartRow":     startRow,
			"Limit":        limit,
			"SubSystemId":  "",
		})
		if err != nil {
			return nil, fmt.Errorf("bill query at row %d: %w", startRow, err)
		}
		logger.Info("kingdee page fetched", "start_row", startRow, "rows", len(rows), "total", len(records)+len(rows), "elapsed", time.Since(pageStarted).Round(time.Millisecond))
		for _, row := range rows {
			if len(row) != len(columns) {
				return nil, fmt.Errorf("kingdee returned %d values for %d fields", len(row), len(columns))
			}
			record := make(map[string]any, len(columns))
			for i, column := range columns {
				record[column] = row[i]
			}
			records = append(records, record)
		}
		if len(rows) < limit {
			break
		}
	}
	return map[string]any{"records": records, "count": len(records)}, nil
}

func clientConfig(info workflow.RuntimeInfo) (kingdee.Config, error) {
	config := kingdee.Config{
		ServerURL: strings.TrimSpace(info.Variables["server_url"]),
		AcctID:    strings.TrimSpace(info.Variables["acct_id"]),
		UserName:  strings.TrimSpace(info.Variables["user_name"]),
		AppID:     strings.TrimSpace(info.Variables["app_id"]),
		AppSecret: strings.TrimSpace(info.Secrets["app_secret"]),
	}
	if config.ServerURL == "" {
		return kingdee.Config{}, errors.New("kingdee server_url variable is required")
	}
	if config.AcctID == "" {
		return kingdee.Config{}, errors.New("kingdee acct_id variable is required")
	}
	if config.UserName == "" {
		return kingdee.Config{}, errors.New("kingdee user_name variable is required")
	}
	if config.AppID == "" {
		return kingdee.Config{}, errors.New("kingdee app_id variable is required")
	}
	if config.AppSecret == "" {
		return kingdee.Config{}, errors.New("kingdee app_secret secret is required")
	}
	if orgNum := strings.TrimSpace(info.Variables["org_num"]); orgNum != "" {
		parsed, err := strconv.Atoi(orgNum)
		if err != nil {
			return kingdee.Config{}, fmt.Errorf("kingdee org_num must be a number, got %q", orgNum)
		}
		config.OrgNum = parsed
	}
	if skip := strings.TrimSpace(info.Variables["skip_tls_verify"]); skip != "" {
		parsed, err := strconv.ParseBool(skip)
		if err != nil {
			return kingdee.Config{}, fmt.Errorf("kingdee skip_tls_verify must be a boolean, got %q", skip)
		}
		config.SkipTLSVerify = parsed
	}
	return config, nil
}

func queryFields(input map[string]any) (fields []string, columns []string, err error) {
	custom := stringSliceInput(input, "field_keys")
	if len(custom) == 0 {
		for _, field := range defaultFields {
			fields = append(fields, field.Field)
			columns = append(columns, field.Column)
		}
		return fields, columns, nil
	}
	for index, field := range custom {
		field = strings.TrimSpace(field)
		if field == "" {
			return nil, nil, fmt.Errorf("kingdee field_keys[%d] must not be empty", index)
		}
		fields = append(fields, field)
		columns = append(columns, field)
	}
	return fields, columns, nil
}

func pageLimit(input map[string]any) (int, error) {
	value, ok := input["limit"]
	if !ok || value == nil {
		return maxPageSize, nil
	}
	var limit int
	switch typed := value.(type) {
	case int:
		limit = typed
	case int64:
		limit = int(typed)
	case float64:
		limit = int(typed)
	default:
		return 0, fmt.Errorf("kingdee limit must be a number, got %T", value)
	}
	if limit <= 0 || limit > maxPageSize {
		return 0, fmt.Errorf("kingdee limit must be between 1 and %d, got %d", maxPageSize, limit)
	}
	return limit, nil
}

func stringInput(input map[string]any, key string) string {
	if value, ok := input[key].(string); ok {
		return value
	}
	return ""
}

func stringSliceInput(input map[string]any, key string) []string {
	switch typed := input[key].(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

var _ workflow.Node = Node{}
