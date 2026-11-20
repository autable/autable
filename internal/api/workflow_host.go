package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"gorm.io/gorm"

	"autable/internal/auth"
	"autable/internal/egress"
	"autable/internal/systemdb"
	"autable/internal/table"
	"autable/internal/wfruntime"
)

// workflowHost is the wfruntime.Host of one run: table, user and file
// operations execute as the workflow:<id> subject through the same checks
// as the HTTP API (docs/permissions.md); fetch goes out directly or
// through a runner of the workflow's database.
type workflowHost struct {
	server   *Server
	workflow systemdb.Workflow
	subject  string
}

func (host *workflowHost) Fetch(ctx context.Context, via string, request egress.Request) (egress.Response, error) {
	if via == "" {
		return egress.Do(ctx, host.server.workflows.client, request)
	}
	return host.server.runnerHub.Fetch(ctx, host.workflow.DatabaseName, via, request)
}

// decodeArgs converts host call arguments into a request struct, rejecting
// unknown keys so mistakes in the SDK fail loudly.
func decodeArgs(op string, args map[string]any, target any) error {
	encoded, err := json.Marshal(args)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	// Plain numbers (float64), as the HTTP row endpoints decode them: the
	// table service normalizes those, not json.Number.
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%s arguments: %w", op, err)
	}
	return nil
}

// hostError names permission and visibility failures for the code.
func hostError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, table.ErrPermissionDenied):
		return &wfruntime.HostError{Name: "PermissionError", Err: err}
	case errors.Is(err, table.ErrRecordNotFound):
		return &wfruntime.HostError{Name: "NotFoundError", Err: err}
	default:
		return err
	}
}

type recordArgs struct {
	RecordID int64          `json:"record_id"`
	Values   map[string]any `json:"values,omitempty"`
}

func (host *workflowHost) Table(ctx context.Context, op, tableName string, args map[string]any) (any, error) {
	result, err := host.table(ctx, op, tableName, args)
	return result, hostError(err)
}

func (host *workflowHost) table(ctx context.Context, op, tableName string, args map[string]any) (any, error) {
	server, dbName, subject := host.server, host.workflow.DatabaseName, host.subject
	switch op {
	case "list", "page":
		var request listRowsRequest
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		options := table.RowListOptions{ViewName: request.View, Query: request.Query, Sorts: request.Sorts, Limit: request.Limit, Offset: request.Offset, Search: request.Search}
		if op == "list" {
			rows, err := server.listTableRowsAs(ctx, subject, dbName, tableName, options)
			if err != nil {
				return nil, err
			}
			return rowResponses(rows), nil
		}
		if request.Limit <= 0 {
			return nil, errors.New("page needs a positive limit")
		}
		rows, total, err := server.listTableRowsPageAs(ctx, subject, dbName, tableName, options)
		if err != nil {
			return nil, err
		}
		return rowPageResponse{Rows: rowResponses(rows), Total: total}, nil
	case "create":
		var request createRowRequest
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		row, err := server.createTableRowAs(ctx, subject, dbName, tableName, request.Values)
		if err != nil {
			return nil, err
		}
		return rowResponse{RecordID: row.RecordID, Values: row.Values}, nil
	case "update":
		var request recordArgs
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		row, err := server.updateTableRowAs(ctx, subject, dbName, tableName, request.RecordID, request.Values)
		if err != nil {
			return nil, err
		}
		return rowResponse{RecordID: row.RecordID, Values: row.Values}, nil
	case "upsert":
		var request upsertRowRequest
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		if _, ok := request.Values[request.MatchField]; request.MatchField == "" || !ok {
			return nil, errors.New("upsert needs match_field and a value for it in values")
		}
		row, operation, err := server.upsertTableRowAs(ctx, subject, dbName, tableName, request.MatchField, request.Values)
		if err != nil {
			return nil, err
		}
		return rowMutationResponse{Operation: operation, RecordID: row.RecordID, Values: row.Values}, nil
	case "delete", "history":
		var request recordArgs
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		if request.Values != nil {
			return nil, fmt.Errorf("%s takes no values", op)
		}
		if op == "history" {
			return server.rowHistoryAs(ctx, subject, dbName, tableName, request.RecordID)
		}
		row, err := server.deleteTableRowAs(ctx, subject, dbName, tableName, request.RecordID)
		if err != nil {
			return nil, err
		}
		return rowResponse{RecordID: row.RecordID, Values: row.Values}, nil
	default:
		return nil, fmt.Errorf("unknown table operation %q", op)
	}
}

func (host *workflowHost) Users(ctx context.Context, op string, args map[string]any) (any, error) {
	system := host.server.system
	found := func(user auth.User, err error) (any, error) {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return toUserResponse(user), nil
	}
	switch op {
	case "get":
		var request struct {
			ID string `json:"id"`
		}
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		return found(system.User(ctx, request.ID))
	case "byEmail":
		var request struct {
			Email string `json:"email"`
		}
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		if _, err := auth.NormalizeEmail(request.Email); err != nil {
			// An unparsable email simply matches nobody.
			return nil, nil
		}
		return found(system.UserByEmail(ctx, request.Email))
	case "search":
		var request struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		if request.Limit <= 0 {
			request.Limit = 20
		}
		users, err := system.SearchUsers(ctx, request.Query, min(request.Limit, 100))
		if err != nil {
			return nil, err
		}
		responses := make([]userResponse, 0, len(users))
		for _, user := range users {
			responses = append(responses, toUserResponse(user))
		}
		return responses, nil
	default:
		return nil, fmt.Errorf("unknown users operation %q", op)
	}
}

func (host *workflowHost) Files(ctx context.Context, op string, args map[string]any) (any, error) {
	result, err := host.files(ctx, op, args)
	return result, hostError(err)
}

func (host *workflowHost) files(ctx context.Context, op string, args map[string]any) (any, error) {
	server, subject := host.server, host.subject
	switch op {
	case "metadata":
		var request fileMetadataRequest
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		return server.fileMetadataAs(ctx, subject, request.IDs)
	case "read":
		var request struct {
			ID int64 `json:"id"`
		}
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		if server.files == nil {
			return nil, errors.New("file storage is not configured")
		}
		record, err := server.system.File(ctx, request.ID)
		if err != nil {
			return nil, err
		}
		if !server.canViewFile(ctx, subject, record) {
			return nil, fmt.Errorf("%w: file %d", table.ErrPermissionDenied, record.ID)
		}
		if record.Size > egress.MaxResponseBytes {
			return nil, fmt.Errorf("file %d is %d bytes; workflows read at most %d", record.ID, record.Size, egress.MaxResponseBytes)
		}
		body, err := server.files.Get(ctx, record.ID, record.Name)
		if err != nil {
			return nil, fmt.Errorf("load file object: %w", err)
		}
		defer body.Close()
		data, err := io.ReadAll(io.LimitReader(body, egress.MaxResponseBytes+1))
		if err != nil {
			return nil, err
		}
		return map[string]any{"file": record, "body": data}, nil
	case "upload":
		var request struct {
			Table       string `json:"table"`
			Name        string `json:"name"`
			Body        []byte `json:"body"`
			ContentType string `json:"content_type"`
			RecordID    int64  `json:"record_id"`
		}
		if err := decodeArgs(op, args, &request); err != nil {
			return nil, err
		}
		if server.files == nil {
			return nil, errors.New("file storage is not configured")
		}
		if int64(len(request.Body)) > server.uploadLimit() {
			return nil, fmt.Errorf("file is %d bytes, the upload limit is %d bytes", len(request.Body), server.uploadLimit())
		}
		return server.storeFileAs(ctx, subject, host.workflow.DatabaseName, request.Table, request.RecordID, request.Name, request.ContentType, request.Body)
	default:
		return nil, fmt.Errorf("unknown files operation %q", op)
	}
}
