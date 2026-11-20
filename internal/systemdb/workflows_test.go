package systemdb

import (
	"context"
	"errors"
	"testing"

	"autable/internal/permission"
)

func TestWorkflowVersionsPublishSettingsAndSecrets(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	workflow, err := db.CreateWorkflow(ctx, Workflow{DatabaseName: "db", Name: "sync", Title: "Sync", CreatorID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateWorkflow(ctx, Workflow{DatabaseName: "db", Name: "sync"}); err == nil {
		t.Fatal("expected duplicate names to be rejected")
	}
	workflow, v1, err := db.AddWorkflowVersion(ctx, workflow.ID, "Sync v1", WorkflowVersion{SDK: "1.0", Message: "first", CreatedBy: "u1"}, []byte("bundle-1"))
	if err != nil {
		t.Fatal(err)
	}
	if v1.Version != 1 || v1.Size != 8 || workflow.LatestVersion != 1 || workflow.PublishedVersion != 0 || workflow.Title != "Sync v1" {
		t.Fatalf("unexpected first version %#v / %#v", v1, workflow)
	}
	if _, _, err := db.AddWorkflowVersion(ctx, workflow.ID, "", WorkflowVersion{SDK: "1.0"}, []byte("bundle-2")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishWorkflowVersion(ctx, workflow.ID, 3, `[]`, nil); !errors.Is(err, ErrWorkflowVersionNotFound) {
		t.Fatalf("expected a missing version error, got %v", err)
	}
	published, err := db.PublishWorkflowVersion(ctx, workflow.ID, 2, `[{"name":"start","kind":"manual"}]`, []string{"text", "text"})
	if err != nil || published.PublishedVersion != 2 {
		t.Fatalf("unexpected publish result %#v, %v", published, err)
	}
	version, err := db.WorkflowVersion(ctx, workflow.ID, 2)
	if err != nil || version.Triggers != `[{"name":"start","kind":"manual"}]` || len(version.Libraries) != 1 || version.Libraries[0] != "text" {
		t.Fatalf("expected triggers stored with the version, got %#v, %v", version, err)
	}
	bundle, err := db.WorkflowVersionBundle(ctx, workflow.ID, 1)
	if err != nil || string(bundle) != "bundle-1" {
		t.Fatalf("unexpected bundle %q, %v", bundle, err)
	}
	versions, err := db.WorkflowVersions(ctx, workflow.ID)
	if err != nil || len(versions) != 2 || versions[0].Version != 2 {
		t.Fatalf("expected versions newest first, got %#v, %v", versions, err)
	}

	disabled, timeout := false, int64(30)
	updated, err := db.UpdateWorkflowSettings(ctx, workflow.ID, WorkflowSettings{Enabled: &disabled, TimeoutSeconds: &timeout})
	if err != nil || updated.Enabled || updated.TimeoutSeconds != 30 || updated.HistoryRetentionDays != DefaultWorkflowRetentionDays {
		t.Fatalf("unexpected settings %#v, %v", updated, err)
	}
	invalid := int64(0)
	if _, err := db.UpdateWorkflowSettings(ctx, workflow.ID, WorkflowSettings{HistoryRetentionDays: &invalid}); err == nil {
		t.Fatal("expected an invalid retention to be rejected")
	}

	if err := db.SetWorkflowSecret(ctx, workflow.ID, "token", "a"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetWorkflowSecret(ctx, workflow.ID, "token", "b"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetWorkflowVar(ctx, workflow.ID, "channel", "ops"); err != nil {
		t.Fatal(err)
	}
	secrets, err := db.WorkflowSecrets(ctx, workflow.ID)
	if err != nil || len(secrets) != 1 || secrets["token"] != "b" {
		t.Fatalf("unexpected secrets %#v, %v", secrets, err)
	}
	if removed, err := db.DeleteWorkflowSecret(ctx, workflow.ID, "token"); err != nil || !removed {
		t.Fatalf("expected the secret to be removed, got %v %v", removed, err)
	}
	if removed, err := db.DeleteWorkflowSecret(ctx, workflow.ID, "token"); err != nil || removed {
		t.Fatalf("expected a second removal to report nothing, got %v %v", removed, err)
	}
	vars, err := db.WorkflowVars(ctx, workflow.ID)
	if err != nil || vars["channel"] != "ops" {
		t.Fatalf("unexpected vars %#v, %v", vars, err)
	}
}

func TestWorkflowRunsLifecycleRetentionAndDelete(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	workflow, err := db.CreateWorkflow(ctx, Workflow{DatabaseName: "db", Name: "sync"})
	if err != nil {
		t.Fatal(err)
	}

	first, err := db.CreateWorkflowRun(ctx, WorkflowRun{WorkflowID: workflow.ID, Version: 1, Trigger: "start", Event: `{"input":{}}`, StartedBy: "u1"})
	if err != nil || first.Status != WorkflowRunQueued {
		t.Fatalf("unexpected new run %#v, %v", first, err)
	}
	started, err := db.StartWorkflowRun(ctx, first.ID, 10)
	if err != nil || !started {
		t.Fatalf("expected the run to start, got %v %v", started, err)
	}
	if again, _ := db.StartWorkflowRun(ctx, first.ID, 11); again {
		t.Fatal("expected a running run not to start twice")
	}
	if err := db.AddWorkflowLogs(ctx, []WorkflowLog{{RunID: first.ID, Time: 12, Level: "info", Message: "hello", Data: `{"a":1}`}}); err != nil {
		t.Fatal(err)
	}
	finished, err := db.FinishWorkflowRun(ctx, first.ID, WorkflowRunSucceeded, `{"ok":true}`, "", `[{"name":"text","version":2}]`, 20)
	if err != nil || !finished {
		t.Fatalf("expected the run to finish, got %v %v", finished, err)
	}
	if again, _ := db.FinishWorkflowRun(ctx, first.ID, WorkflowRunFailed, "", "late", "", 21); again {
		t.Fatal("expected a finished run to stay finished")
	}
	run, err := db.WorkflowRun(ctx, workflow.ID, first.ID)
	if err != nil || run.Status != WorkflowRunSucceeded || run.Output != `{"ok":true}` || run.Event != `{"input":{}}` || run.StartedAt != 10 || run.FinishedAt != 20 || run.Libraries != `[{"name":"text","version":2}]` {
		t.Fatalf("unexpected stored run %#v, %v", run, err)
	}
	if _, err := db.WorkflowRun(ctx, workflow.ID+1, first.ID); !errors.Is(err, ErrWorkflowRunNotFound) {
		t.Fatalf("expected runs to be scoped to their workflow, got %v", err)
	}
	logs, err := db.WorkflowLogs(ctx, first.ID)
	if err != nil || len(logs) != 1 || logs[0].Message != "hello" || logs[0].Data != `{"a":1}` {
		t.Fatalf("unexpected logs %#v, %v", logs, err)
	}

	queued, _ := db.CreateWorkflowRun(ctx, WorkflowRun{WorkflowID: workflow.ID, Trigger: "start"})
	running, _ := db.CreateWorkflowRun(ctx, WorkflowRun{WorkflowID: workflow.ID, Trigger: "start"})
	if _, err := db.StartWorkflowRun(ctx, running.ID, 30); err != nil {
		t.Fatal(err)
	}
	list, err := db.WorkflowRuns(ctx, workflow.ID, WorkflowRunFilter{})
	if err != nil || len(list) != 3 || list[0].ID != running.ID || list[0].Event != "" {
		t.Fatalf("expected runs newest first without events, got %#v, %v", list, err)
	}
	page, err := db.WorkflowRuns(ctx, workflow.ID, WorkflowRunFilter{Before: running.ID, Limit: 1})
	if err != nil || len(page) != 1 || page[0].ID != queued.ID {
		t.Fatalf("unexpected page %#v, %v", page, err)
	}
	onlySucceeded, err := db.WorkflowRuns(ctx, workflow.ID, WorkflowRunFilter{Status: WorkflowRunSucceeded})
	if err != nil || len(onlySucceeded) != 1 || onlySucceeded[0].ID != first.ID {
		t.Fatalf("unexpected status filter %#v, %v", onlySucceeded, err)
	}

	// Retention never touches unfinished runs.
	deleted, err := db.DeleteFinishedWorkflowRunsBefore(ctx, workflow.ID, 1<<62)
	if err != nil || deleted != 1 {
		t.Fatalf("expected one finished run deleted, got %d, %v", deleted, err)
	}
	if logs, _ := db.WorkflowLogs(ctx, first.ID); len(logs) != 0 {
		t.Fatalf("expected the run's logs to go with it, got %#v", logs)
	}
	interrupted, err := db.FailInterruptedWorkflowRuns(ctx, "interrupted by server restart", 40)
	if err != nil || interrupted != 2 {
		t.Fatalf("expected both unfinished runs marked failed, got %d, %v", interrupted, err)
	}
	run, _ = db.WorkflowRun(ctx, workflow.ID, queued.ID)
	if run.Status != WorkflowRunFailed || run.Error != "interrupted by server restart" || run.FinishedAt != 40 {
		t.Fatalf("unexpected interrupted run %#v", run)
	}

	// Deleting removes everything hanging off the workflow.
	subject := WorkflowSubjectID(workflow.ID)
	if err := db.SaveGrant(ctx, permission.Grant{SubjectID: subject, Scope: permission.ScopeFieldSet, Resource: "db.contacts", Level: permission.FieldAll}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveGrant(ctx, permission.Grant{SubjectID: "u1", Scope: permission.ScopeWorkflow, Resource: "1", Level: permission.Run}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveRole(ctx, RoleDefinition{DatabaseName: "db", Name: "sync"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ReplaceRoleMembers(ctx, "db", "sync", []RoleMember{{Type: "workflow", ID: "1"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetWorkflowSecret(ctx, workflow.ID, "token", "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteWorkflow(ctx, workflow.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Workflow(ctx, workflow.ID); err == nil {
		t.Fatal("expected the workflow to be gone")
	}
	for _, subjectID := range []string{subject, "u1"} {
		if grants, _ := db.GrantListForSubject(ctx, subjectID); len(grants) != 0 {
			t.Fatalf("expected grants of %s removed, got %#v", subjectID, grants)
		}
	}
	if role, _ := db.Role(ctx, "db", "sync"); len(role.Members) != 0 {
		t.Fatalf("expected the role membership removed, got %#v", role.Members)
	}
	if runs, _ := db.WorkflowRuns(ctx, workflow.ID, WorkflowRunFilter{}); len(runs) != 0 {
		t.Fatalf("expected runs removed, got %#v", runs)
	}
	if secrets, _ := db.WorkflowSecrets(ctx, workflow.ID); len(secrets) != 0 {
		t.Fatalf("expected secrets removed, got %#v", secrets)
	}
}
