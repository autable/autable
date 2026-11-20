package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"

	"autable/internal/appbundle"
	"autable/internal/cron"
	"autable/internal/egress"
	"autable/internal/history"
	"autable/internal/systemdb"
	"autable/internal/wfruntime"
)

// The workflow engine runs workflow bundles: an in-memory queue drained by
// a worker pool, webhook runs that answer their request directly, the
// schedule ticker, recordChanged dispatch and run retention. Runs are
// stateless (docs/design/workflows-v2.md): each is one execution of
// run(event) in a fresh runtime, recorded in the system database.

const (
	// DefaultWorkflowWorkers bounds the runs executing at once.
	DefaultWorkflowWorkers = 8
	interruptedRunError    = "interrupted by server restart"
	programCacheSize       = 32
	retentionInterval      = 24 * time.Hour
)

type queuedRun struct {
	run      systemdb.WorkflowRun
	workflow systemdb.Workflow
}

type activeRun struct {
	workflowID int64
	cancel     context.CancelFunc
	cancelled  atomic.Bool
}

type workflowEngine struct {
	server *Server
	client *http.Client
	now    func() time.Time
	// slots bounds concurrent executions, queued and webhook runs alike.
	slots chan struct{}

	mu        sync.Mutex
	ctx       context.Context
	queue     []queuedRun
	wake      chan struct{}
	active    map[int64]*activeRun
	done      map[int64]chan struct{}
	programs  map[[2]int64]*goja.Program
	triggers  map[[2]int64][]wfruntime.Trigger
	lastFired map[string]int64
	wg        sync.WaitGroup
}

func newWorkflowEngine(server *Server) *workflowEngine {
	return &workflowEngine{
		server:    server,
		client:    egress.NewClient(),
		now:       time.Now,
		slots:     make(chan struct{}, DefaultWorkflowWorkers),
		wake:      make(chan struct{}, 1),
		active:    map[int64]*activeRun{},
		done:      map[int64]chan struct{}{},
		programs:  map[[2]int64]*goja.Program{},
		triggers:  map[[2]int64][]wfruntime.Trigger{},
		lastFired: map[string]int64{},
	}
}

// StartWorkflows marks runs a previous process left unfinished as failed
// and starts the workers, the schedule ticker and run retention. They stop
// when ctx ends; WaitWorkflows waits for them.
func (server *Server) StartWorkflows(ctx context.Context) error {
	engine := server.workflows
	engine.mu.Lock()
	if engine.ctx != nil {
		engine.mu.Unlock()
		return errors.New("workflows already started")
	}
	// Runs this process queued before starting are not interrupted ones.
	keep := make([]int64, 0, len(engine.queue))
	for _, item := range engine.queue {
		keep = append(keep, item.run.ID)
	}
	count, err := server.system.FailInterruptedWorkflowRuns(ctx, interruptedRunError, engine.now().UnixMilli(), keep...)
	if err == nil {
		engine.ctx = ctx
	}
	engine.mu.Unlock()
	if err != nil {
		return err
	}
	if count > 0 {
		slog.Warn("workflow runs interrupted by the previous shutdown were marked failed", "runs", count)
	}
	for range cap(engine.slots) {
		engine.wg.Add(1)
		go engine.worker(ctx)
	}
	engine.wg.Add(2)
	go engine.scheduleLoop(ctx)
	go engine.retentionLoop(ctx)
	return nil
}

// WaitWorkflows waits until the goroutines started by StartWorkflows have
// stopped after their context ended.
func (server *Server) WaitWorkflows() {
	server.workflows.wg.Wait()
}

// SetWorkflowClock replaces the clock used for timestamps and schedules
// (tests).
func (server *Server) SetWorkflowClock(now func() time.Time) {
	server.workflows.now = now
}

func (engine *workflowEngine) running() context.Context {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.ctx
}

// enqueue records a queued run of the published version and hands it to
// the workers.
func (engine *workflowEngine) enqueue(ctx context.Context, workflow systemdb.Workflow, trigger string, event map[string]any, startedBy string) (systemdb.WorkflowRun, error) {
	encoded, err := json.Marshal(event)
	if err != nil {
		return systemdb.WorkflowRun{}, err
	}
	run, err := engine.server.system.CreateWorkflowRun(context.WithoutCancel(ctx), systemdb.WorkflowRun{
		WorkflowID: workflow.ID,
		Version:    workflow.PublishedVersion,
		Trigger:    trigger,
		Event:      string(encoded),
		StartedBy:  startedBy,
	})
	if err != nil {
		return systemdb.WorkflowRun{}, err
	}
	engine.mu.Lock()
	engine.queue = append(engine.queue, queuedRun{run: run, workflow: workflow})
	engine.done[run.ID] = make(chan struct{})
	engine.mu.Unlock()
	select {
	case engine.wake <- struct{}{}:
	default:
	}
	return run, nil
}

func (engine *workflowEngine) worker(ctx context.Context) {
	defer engine.wg.Done()
	for {
		engine.mu.Lock()
		var next *queuedRun
		if len(engine.queue) > 0 {
			item := engine.queue[0]
			engine.queue = engine.queue[1:]
			next = &item
		}
		engine.mu.Unlock()
		if next == nil {
			select {
			case <-ctx.Done():
				return
			case <-engine.wake:
				continue
			}
		}
		// Another worker may be waiting on the wake signal too.
		select {
		case engine.wake <- struct{}{}:
		default:
		}
		select {
		case engine.slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		engine.execute(ctx, next.workflow, next.run)
		<-engine.slots
	}
}

// execute runs a queued run to its end and records the outcome.
func (engine *workflowEngine) execute(ctx context.Context, workflow systemdb.Workflow, run systemdb.WorkflowRun) {
	defer engine.markDone(run.ID)
	system := engine.server.system
	store := context.WithoutCancel(ctx)
	started, err := system.StartWorkflowRun(store, run.ID, engine.now().UnixMilli())
	if err != nil || !started {
		return
	}
	if current, err := system.Workflow(store, workflow.ID); err == nil {
		workflow.TimeoutSeconds = current.TimeoutSeconds
	}
	var event map[string]any
	decoder := json.NewDecoder(strings.NewReader(run.Event))
	decoder.UseNumber()
	if err := decoder.Decode(&event); err != nil {
		engine.finish(store, run, wfruntime.Result{}, fmt.Errorf("decode stored event: %w", err), false)
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	active := engine.track(run.ID, workflow.ID, cancel)
	result, runErr := engine.runProgram(runCtx, workflow, run, event, false, time.Duration(workflow.TimeoutSeconds)*time.Second)
	engine.untrack(run.ID)
	if ctx.Err() != nil && !active.wasCancelled() {
		// Shutdown: leave the run running; the next start marks it
		// interrupted.
		return
	}
	engine.finish(store, run, result, runErr, active.wasCancelled())
}

func (engine *workflowEngine) track(runID, workflowID int64, cancel context.CancelFunc) *activeRun {
	active := &activeRun{workflowID: workflowID, cancel: cancel}
	engine.mu.Lock()
	engine.active[runID] = active
	engine.mu.Unlock()
	return active
}

func (engine *workflowEngine) untrack(runID int64) {
	engine.mu.Lock()
	delete(engine.active, runID)
	engine.mu.Unlock()
}

func (active *activeRun) wasCancelled() bool {
	return active.cancelled.Load()
}

// finish stores the logs and the final status of a run.
func (engine *workflowEngine) finish(ctx context.Context, run systemdb.WorkflowRun, result wfruntime.Result, runErr error, cancelled bool) {
	system := engine.server.system
	logs := make([]systemdb.WorkflowLog, 0, len(result.Logs))
	for _, entry := range result.Logs {
		logs = append(logs, systemdb.WorkflowLog{RunID: run.ID, Time: entry.Time, Level: entry.Level, Message: entry.Message, Data: entry.Data})
	}
	if err := system.AddWorkflowLogs(ctx, logs); err != nil {
		slog.Error("store workflow run logs", "run", run.ID, "error", err)
	}
	status, errorText := systemdb.WorkflowRunSucceeded, ""
	switch {
	case cancelled:
		status, errorText = systemdb.WorkflowRunCancelled, "cancelled"
	case runErr != nil:
		status, errorText = systemdb.WorkflowRunFailed, runErr.Error()
	}
	output := result.Output
	if result.Response != nil {
		encoded, _ := json.Marshal(map[string]any{"status": result.Response.Status, "headers": result.Response.Headers})
		output = string(encoded)
	}
	if _, err := system.FinishWorkflowRun(ctx, run.ID, status, output, errorText, engine.now().UnixMilli()); err != nil {
		slog.Error("store workflow run result", "run", run.ID, "error", err)
	}
}

func (engine *workflowEngine) markDone(runID int64) {
	engine.mu.Lock()
	if done, ok := engine.done[runID]; ok {
		close(done)
		delete(engine.done, runID)
	}
	engine.mu.Unlock()
}

// wait blocks until the run finished or ctx ends, reporting whether it
// finished.
func (engine *workflowEngine) wait(ctx context.Context, runID int64) bool {
	engine.mu.Lock()
	done, ok := engine.done[runID]
	engine.mu.Unlock()
	if !ok {
		return true
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// cancel stops a run: a queued run is cancelled before it starts, a
// running one is interrupted and recorded as cancelled.
func (engine *workflowEngine) cancel(ctx context.Context, runID int64) error {
	engine.mu.Lock()
	active, running := engine.active[runID]
	if running {
		active.cancelled.Store(true)
	}
	engine.mu.Unlock()
	if running {
		active.cancel()
		waitCtx, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		engine.wait(waitCtx, runID)
		return nil
	}
	_, err := engine.server.system.FinishWorkflowRun(ctx, runID, systemdb.WorkflowRunCancelled, "", "cancelled", engine.now().UnixMilli())
	if err == nil {
		engine.markDone(runID)
	}
	return err
}

// cancelWorkflow drops the queued runs and interrupts the active runs of
// a workflow being deleted.
func (engine *workflowEngine) cancelWorkflow(workflowID int64) {
	engine.mu.Lock()
	kept := engine.queue[:0]
	for _, item := range engine.queue {
		if item.workflow.ID == workflowID {
			if done, ok := engine.done[item.run.ID]; ok {
				close(done)
				delete(engine.done, item.run.ID)
			}
			continue
		}
		kept = append(kept, item)
	}
	engine.queue = kept
	for _, active := range engine.active {
		if active.workflowID == workflowID {
			active.cancelled.Store(true)
			active.cancel()
		}
	}
	engine.mu.Unlock()
}

// runWebhook executes a webhook run right away, bounded by the webhook
// time limit, and returns its HTTP response.
func (engine *workflowEngine) runWebhook(ctx context.Context, workflow systemdb.Workflow, trigger string, event map[string]any) (*wfruntime.WebhookResponse, systemdb.WorkflowRun, error) {
	system := engine.server.system
	store := context.WithoutCancel(ctx)
	encoded, err := json.Marshal(event)
	if err != nil {
		return nil, systemdb.WorkflowRun{}, err
	}
	run, err := system.CreateWorkflowRun(store, systemdb.WorkflowRun{WorkflowID: workflow.ID, Version: workflow.PublishedVersion, Trigger: trigger, Event: string(encoded)})
	if err != nil {
		return nil, systemdb.WorkflowRun{}, err
	}
	limit := min(webhookTimeout, time.Duration(workflow.TimeoutSeconds)*time.Second)
	runCtx, cancel := context.WithTimeout(store, limit)
	defer cancel()
	select {
	case engine.slots <- struct{}{}:
		defer func() { <-engine.slots }()
	case <-runCtx.Done():
		engine.finish(store, run, wfruntime.Result{}, fmt.Errorf("%w: no worker became free in time", wfruntime.ErrTimeout), false)
		return nil, run, wfruntime.ErrTimeout
	}
	if _, err := system.StartWorkflowRun(store, run.ID, engine.now().UnixMilli()); err != nil {
		return nil, run, err
	}
	active := engine.track(run.ID, workflow.ID, cancel)
	result, runErr := engine.runProgram(runCtx, workflow, run, event, true, limit)
	engine.untrack(run.ID)
	engine.finish(store, run, result, runErr, active.wasCancelled())
	if runErr != nil {
		return nil, run, runErr
	}
	return result.Response, run, nil
}

// runProgram executes one run of the pinned version.
func (engine *workflowEngine) runProgram(ctx context.Context, workflow systemdb.Workflow, run systemdb.WorkflowRun, event map[string]any, webhook bool, timeout time.Duration) (wfruntime.Result, error) {
	system := engine.server.system
	program, err := engine.program(ctx, workflow.ID, run.Version)
	if err != nil {
		return wfruntime.Result{}, err
	}
	secrets, err := system.WorkflowSecrets(ctx, workflow.ID)
	if err != nil {
		return wfruntime.Result{}, err
	}
	vars, err := system.WorkflowVars(ctx, workflow.ID)
	if err != nil {
		return wfruntime.Result{}, err
	}
	full := make(map[string]any, len(event)+2)
	for key, value := range event {
		full[key] = value
	}
	full["trigger"] = run.Trigger
	full["runId"] = run.ID
	return wfruntime.Run(ctx, wfruntime.Options{
		Program: program,
		Info:    wfruntime.Info{Database: workflow.DatabaseName, Workflow: workflow.Name, Version: run.Version, RunID: run.ID, Trigger: run.Trigger},
		Event:   full,
		Secrets: secrets,
		Vars:    vars,
		Host:    &workflowHost{server: engine.server, workflow: workflow, subject: systemdb.WorkflowSubjectID(workflow.ID)},
		Timeout: timeout,
		Webhook: webhook,
	})
}

// program returns the compiled bundle script of a version; versions are
// immutable, so compiled programs are cached.
func (engine *workflowEngine) program(ctx context.Context, workflowID, version int64) (*goja.Program, error) {
	key := [2]int64{workflowID, version}
	engine.mu.Lock()
	program, ok := engine.programs[key]
	engine.mu.Unlock()
	if ok {
		return program, nil
	}
	archive, err := engine.server.system.WorkflowVersionBundle(ctx, workflowID, version)
	if err != nil {
		return nil, err
	}
	bundle, err := appbundle.Workflow.Parse(archive)
	if err != nil {
		return nil, err
	}
	program, err = wfruntime.Compile("workflow.js", string(bundle.Dist[appbundle.Workflow.RequiredDist]))
	if err != nil {
		return nil, err
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.programs) >= programCacheSize {
		for existing := range engine.programs {
			delete(engine.programs, existing)
			break
		}
	}
	engine.programs[key] = program
	return program, nil
}

// publishedTriggers returns the triggers of the published version (none
// while nothing is published).
func (engine *workflowEngine) publishedTriggers(ctx context.Context, workflow systemdb.Workflow) ([]wfruntime.Trigger, error) {
	if workflow.PublishedVersion == 0 {
		return nil, nil
	}
	key := [2]int64{workflow.ID, workflow.PublishedVersion}
	engine.mu.Lock()
	triggers, ok := engine.triggers[key]
	engine.mu.Unlock()
	if ok {
		return triggers, nil
	}
	version, err := engine.server.system.WorkflowVersion(ctx, workflow.ID, workflow.PublishedVersion)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(version.Triggers), &triggers); err != nil {
		return nil, fmt.Errorf("stored triggers of %s version %d: %w", workflow.Name, version.Version, err)
	}
	engine.mu.Lock()
	engine.triggers[key] = triggers
	engine.mu.Unlock()
	return triggers, nil
}

// forgetTriggers drops cached triggers of a version that was published
// again (its triggers are read anew).
func (engine *workflowEngine) forgetTriggers(workflowID, version int64) {
	engine.mu.Lock()
	delete(engine.triggers, [2]int64{workflowID, version})
	engine.mu.Unlock()
}

func (engine *workflowEngine) forgetWorkflow(workflowID int64) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	for key := range engine.programs {
		if key[0] == workflowID {
			delete(engine.programs, key)
		}
	}
	for key := range engine.triggers {
		if key[0] == workflowID {
			delete(engine.triggers, key)
		}
	}
}

// triggerWorkflows lists the enabled, published workflows of a database
// with their triggers.
func (engine *workflowEngine) triggerWorkflows(ctx context.Context, dbName string) ([]systemdb.Workflow, [][]wfruntime.Trigger, error) {
	workflows, err := engine.server.system.Workflows(ctx, dbName)
	if err != nil {
		return nil, nil, err
	}
	var kept []systemdb.Workflow
	var triggers [][]wfruntime.Trigger
	for _, workflow := range workflows {
		if !workflow.Enabled || workflow.PublishedVersion == 0 {
			continue
		}
		declared, err := engine.publishedTriggers(ctx, workflow)
		if err != nil {
			slog.Error("load workflow triggers", "workflow", workflow.Name, "error", err)
			continue
		}
		kept = append(kept, workflow)
		triggers = append(triggers, declared)
	}
	return kept, triggers, nil
}

// Schedules.

func (engine *workflowEngine) scheduleLoop(ctx context.Context) {
	defer engine.wg.Done()
	for {
		now := engine.now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		timer := time.NewTimer(next.Sub(now))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		engine.scheduleTick(ctx, engine.now().Truncate(time.Minute))
	}
}

// scheduleTick queues a run for every schedule trigger matching minute.
// Each trigger fires at most once per minute.
func (engine *workflowEngine) scheduleTick(ctx context.Context, minute time.Time) {
	minute = minute.Truncate(time.Minute)
	for _, database := range engine.server.catalogSnapshot().Databases {
		workflows, triggers, err := engine.triggerWorkflows(ctx, database.Name)
		if err != nil {
			slog.Error("list workflows for schedules", "database", database.Name, "error", err)
			continue
		}
		for index, workflow := range workflows {
			for _, trigger := range triggers[index] {
				if trigger.Kind != wfruntime.KindSchedule {
					continue
				}
				schedule, err := cron.Parse(trigger.Cron, trigger.Timezone)
				if err != nil || !schedule.Matches(minute) {
					continue
				}
				key := fmt.Sprintf("%d/%s", workflow.ID, trigger.Name)
				engine.mu.Lock()
				fired := engine.lastFired[key] >= minute.UnixMilli()
				if !fired {
					engine.lastFired[key] = minute.UnixMilli()
				}
				engine.mu.Unlock()
				if fired {
					continue
				}
				if _, err := engine.enqueue(ctx, workflow, trigger.Name, map[string]any{"scheduledAt": minute.UnixMilli()}, ""); err != nil {
					slog.Error("queue scheduled workflow run", "workflow", workflow.Name, "error", err)
				}
			}
		}
	}
}

// Record changes.

// dispatchRowChange queues a run for every recordChanged trigger the row
// change matches, with the change filtered to what the workflow subject
// may read.
func (engine *workflowEngine) dispatchRowChange(ctx context.Context, historyKey string, change history.RowChange) {
	ctx = context.WithoutCancel(ctx)
	workflows, triggers, err := engine.triggerWorkflows(ctx, change.Database)
	if err != nil {
		slog.Error("list workflows for a row change", "database", change.Database, "error", err)
		return
	}
	var actor map[string]any
	for index, workflow := range workflows {
		subject := systemdb.WorkflowSubjectID(workflow.ID)
		var visible *history.RowChange
		for _, trigger := range triggers[index] {
			if trigger.Kind != wfruntime.KindRecordChanged || trigger.Table != change.Table || !containsString(trigger.Operations, change.Operation) {
				continue
			}
			if change.ActorID == subject && !trigger.IncludeOwnWrites {
				continue
			}
			if visible == nil {
				filtered, ok := engine.server.readableRowChangeForSubject(ctx, subject, change)
				if !ok {
					break
				}
				visible = &filtered
			}
			changed := make([]string, 0, len(visible.Diff))
			for field := range visible.Diff {
				changed = append(changed, field)
			}
			sort.Strings(changed)
			if len(trigger.ChangedFields) > 0 && !anyContained(trigger.ChangedFields, changed) {
				continue
			}
			if actor == nil {
				actor = engine.actorOf(ctx, change.ActorID)
			}
			event := map[string]any{"change": map[string]any{
				"history_key":   historyKey,
				"database":      visible.Database,
				"table":         visible.Table,
				"record_id":     visible.RecordID,
				"timestamp":     visible.Timestamp,
				"operation":     visible.Operation,
				"values":        visible.Values,
				"diff":          visible.Diff,
				"actor_id":      visible.ActorID,
				"actor":         actor,
				"changedFields": changed,
			}}
			if _, err := engine.enqueue(ctx, workflow, trigger.Name, event, ""); err != nil {
				slog.Error("queue workflow run for a row change", "workflow", workflow.Name, "error", err)
			}
		}
	}
}

func (engine *workflowEngine) actorOf(ctx context.Context, actorID string) map[string]any {
	if strings.HasPrefix(actorID, "workflow:") {
		return map[string]any{"id": actorID, "type": "workflow"}
	}
	actor := map[string]any{"id": actorID, "type": "user"}
	if user, err := engine.server.system.User(ctx, actorID); err == nil {
		actor["email"] = user.Email
		actor["display_name"] = user.DisplayName
	}
	return actor
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func anyContained(wanted, values []string) bool {
	for _, value := range wanted {
		if containsString(values, value) {
			return true
		}
	}
	return false
}

// Retention.

func (engine *workflowEngine) retentionLoop(ctx context.Context) {
	defer engine.wg.Done()
	ticker := time.NewTicker(retentionInterval)
	defer ticker.Stop()
	for {
		if err := engine.server.CleanupWorkflowRuns(ctx); err != nil && ctx.Err() == nil {
			slog.Error("workflow run cleanup failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// CleanupWorkflowRuns deletes finished runs older than each workflow's
// retention. Queued and running runs are never deleted.
func (server *Server) CleanupWorkflowRuns(ctx context.Context) error {
	now := server.workflows.now()
	deleted := 0
	for _, database := range server.catalogSnapshot().Databases {
		workflows, err := server.system.Workflows(ctx, database.Name)
		if err != nil {
			return err
		}
		for _, workflow := range workflows {
			cutoff := now.Add(-time.Duration(workflow.HistoryRetentionDays) * 24 * time.Hour).UnixMilli()
			count, err := server.system.DeleteFinishedWorkflowRunsBefore(ctx, workflow.ID, cutoff)
			if err != nil {
				return err
			}
			deleted += count
		}
	}
	if deleted > 0 {
		slog.Info("workflow run cleanup finished", "deleted", deleted)
	}
	return nil
}
