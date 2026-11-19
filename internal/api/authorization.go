package api

import (
	"context"
	"fmt"
	"net/http"

	"autable/internal/permission"
	"autable/internal/systemdb"
	"autable/internal/table"
)

// autable is admin-gated (docs/permissions.md): the administrators listed
// in auth.admins are the only people who change code or structure —
// databases, schema, views, permissions, workflows, apps. Everyone else
// works with data inside the boundaries admins grant, opens the apps and
// runs the workflows they are granted.

// errAdminRequired wraps the permission error so table-layer callers map it
// to 403 like any other denial.
var errAdminRequired = fmt.Errorf("%w: this action requires an admin", table.ErrPermissionDenied)

// isAdmin reports whether the subject is a signed-in user whose email is
// listed in auth.admins. Workflow and role subjects are never admins.
func (server *Server) isAdmin(ctx context.Context, actorID string) bool {
	if actorID == "" || len(server.auth.Admins) == 0 {
		return false
	}
	user, err := server.system.User(ctx, actorID)
	if err != nil {
		return false
	}
	return server.auth.IsAdmin(user.Email)
}

// requireAdmin writes 403 and returns false unless the actor is an admin.
func (server *Server) requireAdmin(w http.ResponseWriter, r *http.Request, actorID string) bool {
	if server.isAdmin(r.Context(), actorID) {
		return true
	}
	writeError(w, http.StatusForbidden, errAdminRequired)
	return false
}

// canRunWorkflow: admins run every workflow; anyone else needs a workflow
// grant (level 1, run) on it.
func canRunWorkflow(perms permission.Set, actorID string, isAdmin bool, workflowID int64) bool {
	return isAdmin || perms.CanReadResource(actorID, permission.ScopeWorkflow, resourceID(workflowID))
}

// canOpenApp: admins open every app; anyone else needs an app_set grant on
// the database or an app grant on the app (level 1, open).
func canOpenApp(perms permission.Set, actorID string, isAdmin bool, app systemdb.AppDefinition) bool {
	return isAdmin ||
		perms.CanReadResource(actorID, permission.ScopeAppSet, app.DatabaseName) ||
		perms.CanReadResource(actorID, permission.ScopeApp, resourceID(app.ID))
}

// resourcePermissionLevel is the level reported on workflow and app
// responses: Write for admins (they manage it), Read (run / open) for
// everyone the resource is listed for.
func resourcePermissionLevel(isAdmin bool) permission.Level {
	if isAdmin {
		return permission.Write
	}
	return permission.Read
}

func tableResource(dbName, tableName string) string {
	if dbName == "" || tableName == "" {
		return ""
	}
	return dbName + "." + tableName
}
