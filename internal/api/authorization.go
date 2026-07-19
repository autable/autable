package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"autable/internal/permission"
	"autable/internal/table"
)

type accessAction string

const (
	accessManageDatabase accessAction = "manage_database"
	accessCreateField    accessAction = "create_field"
	accessWriteFieldSet  accessAction = "write_field_set"
	accessDeleteField    accessAction = "delete_field"
	accessCreateView     accessAction = "create_view"
	accessWriteViewSet   accessAction = "write_view_set"
	accessWriteView      accessAction = "write_view"
	accessDeleteView     accessAction = "delete_view"
	accessCreateWorkflow accessAction = "create_workflow"
	accessReadWorkflow   accessAction = "read_workflow"
	accessWriteWorkflow  accessAction = "write_workflow"
	accessDeleteWorkflow accessAction = "delete_workflow"
	accessCreateForm     accessAction = "create_form"
	accessReadForm       accessAction = "read_form"
	accessWriteForm      accessAction = "write_form"
	accessDeleteForm     accessAction = "delete_form"
)

type accessRequest struct {
	Action     accessAction
	Database   string
	Table      string
	Field      string
	View       string
	WorkflowID int64
	FormID     int64
}

func (server *Server) authorize(ctx context.Context, actorID string, request accessRequest) (permission.Set, error) {
	if actorID == "" {
		return permission.Set{}, errors.New("authentication is required")
	}
	perms, err := server.system.EffectiveGrantsForSubject(ctx, actorID)
	if err != nil {
		return permission.Set{}, err
	}
	if server.isAuthorized(ctx, actorID, perms, request) {
		return perms, nil
	}
	return permission.Set{}, table.ErrPermissionDenied
}

func (server *Server) isAuthorized(ctx context.Context, actorID string, perms permission.Set, request accessRequest) bool {
	if request.Database != "" && server.isDatabaseOwner(ctx, actorID, request.Database) {
		return true
	}
	resource := tableResource(request.Database, request.Table)
	switch request.Action {
	case accessManageDatabase:
		return server.isDatabaseOwner(ctx, actorID, request.Database)
	case accessCreateField, accessWriteFieldSet:
		// Schema-level field management is the field_add metadata
		// permission; data-level field bits never confer it.
		return resource != "" && perms.CanAddFields(actorID, resource)
	case accessDeleteField:
		return server.isDatabaseOwner(ctx, actorID, request.Database)
	case accessCreateView, accessWriteViewSet, accessWriteView:
		// View definitions are the row-level permission boundary; only the
		// database owner (short-circuited above) may change them.
		return false
	case accessDeleteView:
		return server.isDatabaseOwner(ctx, actorID, request.Database)
	case accessCreateWorkflow:
		return perms.CanWriteResource(actorID, permission.ScopeWorkflowSet, request.Database)
	case accessReadWorkflow:
		return server.canAccessWorkflow(ctx, actorID, perms, request.WorkflowID, permission.Read, false)
	case accessWriteWorkflow:
		return server.canAccessWorkflow(ctx, actorID, perms, request.WorkflowID, permission.Write, false)
	case accessDeleteWorkflow:
		return server.canAccessWorkflow(ctx, actorID, perms, request.WorkflowID, permission.Write, true)
	case accessCreateForm:
		return perms.CanWriteResource(actorID, permission.ScopeFormSet, request.Database)
	case accessReadForm:
		return server.canAccessForm(ctx, actorID, perms, request.FormID, permission.Read, false)
	case accessWriteForm:
		return server.canAccessForm(ctx, actorID, perms, request.FormID, permission.Write, false)
	case accessDeleteForm:
		return server.canAccessForm(ctx, actorID, perms, request.FormID, permission.Write, true)
	default:
		return false
	}
}

func (server *Server) canAccessWorkflow(ctx context.Context, actorID string, perms permission.Set, workflowID int64, level permission.Level, deleteOnly bool) bool {
	workflow, err := server.system.Workflow(ctx, workflowID)
	if err != nil {
		return false
	}
	if deleteOnly {
		return server.isDatabaseOwner(ctx, actorID, workflow.DatabaseName)
	}
	if server.isDatabaseOwner(ctx, actorID, workflow.DatabaseName) {
		return true
	}
	if perms.ResourceLevel(actorID, permission.ScopeWorkflowSet, workflow.DatabaseName) >= level {
		return true
	}
	return perms.ResourceLevel(actorID, permission.ScopeWorkflow, resourceID(workflowID)) >= level
}

func (server *Server) canAccessForm(ctx context.Context, actorID string, perms permission.Set, formID int64, level permission.Level, deleteOnly bool) bool {
	form, err := server.system.Form(ctx, formID)
	if err != nil {
		return false
	}
	if deleteOnly {
		return server.isDatabaseOwner(ctx, actorID, form.DatabaseName)
	}
	if server.isDatabaseOwner(ctx, actorID, form.DatabaseName) {
		return true
	}
	if perms.ResourceLevel(actorID, permission.ScopeFormSet, form.DatabaseName) >= level {
		return true
	}
	return perms.ResourceLevel(actorID, permission.ScopeForm, resourceID(formID)) >= level
}

func (server *Server) requireAuthorized(w http.ResponseWriter, r *http.Request, actorID string, request accessRequest) (permission.Set, bool) {
	perms, err := server.authorize(r.Context(), actorID, request)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, table.ErrPermissionDenied) {
			status = http.StatusForbidden
		}
		writeError(w, status, err)
		return permission.Set{}, false
	}
	return perms, true
}

func (server *Server) isDatabaseOwner(ctx context.Context, actorID, dbName string) bool {
	ok, err := server.system.IsDatabaseOwner(ctx, actorID, dbName)
	return err == nil && ok
}

func (server *Server) requireDatabaseOrSetWrite(w http.ResponseWriter, r *http.Request, actorID string, dbName string, scope permission.Scope) bool {
	action := accessManageDatabase
	switch scope {
	case permission.ScopeWorkflowSet:
		action = accessCreateWorkflow
	case permission.ScopeFormSet:
		action = accessCreateForm
	case permission.ScopeFieldSet:
		action = accessWriteFieldSet
	case permission.ScopeViewSet:
		action = accessWriteViewSet
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("unsupported write set scope %q", scope))
		return false
	}
	_, ok := server.requireAuthorized(w, r, actorID, accessRequest{
		Action:   action,
		Database: dbName,
	})
	return ok
}

func tableResource(dbName, tableName string) string {
	if dbName == "" || tableName == "" {
		return ""
	}
	return dbName + "." + tableName
}

func tableNameFromResource(resource string) (string, error) {
	for index, char := range resource {
		if char == '.' {
			if index == 0 || index == len(resource)-1 {
				break
			}
			return resource[index+1:], nil
		}
	}
	return "", fmt.Errorf("grant resource %q must be db.table", resource)
}
