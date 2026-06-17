package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"codetable/internal/history"

	"github.com/dop251/goja"
	"github.com/google/uuid"
)

type Definition struct {
	ID           int64
	DatabaseName string
	Script       string
	CreatorID    string
	Secrets      map[string]string
	Variables    map[string]string
}

var ErrMissingTrigger = errors.New("workflow script must define function trigger(info)")
var ErrMissingInstances = errors.New("workflow script must define function instances(info)")

type InstanceDeclaration struct {
	Node      string         `json:"node"`
	Variables []Port         `json:"variables,omitempty"`
	Secrets   []Port         `json:"secrets,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
}

type TriggerDeclaration struct {
	Instance string         `json:"instance"`
	Node     string         `json:"node"`
	Params   map[string]any `json:"params,omitempty"`
}

type Runner struct {
	store history.Store
	nodes map[string]Node
	now   func() time.Time
}

func NewRunner(store history.Store, nodes ...Node) *Runner {
	runner := &Runner{
		store: store,
		nodes: map[string]Node{},
		now:   func() time.Time { return time.Now().UTC() },
	}
	for _, node := range nodes {
		runner.Register(node)
	}
	return runner
}

func (runner *Runner) Register(node Node) {
	info := node.Info()
	runner.nodes[info.Type] = node
}

func (runner *Runner) NodeInfos() []NodeInfo {
	infos := make([]NodeInfo, 0, len(runner.nodes))
	for _, node := range runner.nodes {
		infos = append(infos, node.Info())
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Type < infos[j].Type
	})
	return infos
}

func (runner *Runner) Run(ctx context.Context, definition Definition, inputs map[string]any) (history.WorkflowRun, string, error) {
	return runner.RunAt(ctx, definition, inputs, runner.now())
}

func (runner *Runner) RunAt(ctx context.Context, definition Definition, inputs map[string]any, timestamp time.Time) (history.WorkflowRun, string, error) {
	runID := uuid.NewString()
	run := history.WorkflowRun{
		WorkflowID: definition.ID,
		Timestamp:  timestamp.UTC().UnixMilli(),
		Inputs:     cloneAnyMap(inputs),
		Steps:      []history.StepRecord{},
	}
	instances, err := runner.Instances(ctx, definition)
	if err != nil {
		return runner.finish(ctx, run, err)
	}
	runtime := newRuntime()
	info := workflowInfo(definition, runID, inputs)
	info["instance"] = func(call goja.FunctionCall) goja.Value {
		instanceID := call.Argument(0).String()
		declaration, ok := instances[instanceID]
		if !ok {
			panic(runtime.ToValue(fmt.Sprintf("workflow instance %q is not declared", instanceID)))
		}
		return runtime.ToValue(map[string]any{
			"id":   instanceID,
			"node": declaration.Node,
			"exec": func(call goja.FunctionCall) goja.Value {
				nodeInput := exportedMap(call.Argument(0).Export())
				output, err := runner.runInstance(ctx, definition, runID, instanceID, declaration, nodeInput, &run)
				if err != nil {
					panic(runtime.ToValue(err.Error()))
				}
				return runtime.ToValue(output)
			},
		})
	}

	if _, err := runtime.RunString(definition.Script); err != nil {
		return runner.finish(ctx, run, err)
	}
	fn, ok := goja.AssertFunction(runtime.Get("run"))
	if !ok {
		return runner.finish(ctx, run, errors.New("workflow script must define function run(info)"))
	}
	output, err := fn(goja.Undefined(), runtime.ToValue(info))
	if err != nil {
		return runner.finish(ctx, run, err)
	}
	run.Outputs = exportedMap(output.Export())
	return runner.finish(ctx, run, nil)
}

func (runner *Runner) Instances(ctx context.Context, definition Definition) (map[string]InstanceDeclaration, error) {
	runtime := newRuntime()
	if _, err := runtime.RunString(definition.Script); err != nil {
		return nil, err
	}
	fn, ok := goja.AssertFunction(runtime.Get("instances"))
	if !ok {
		return nil, ErrMissingInstances
	}
	info := workflowInfo(definition, "", nil)
	delete(info, "run_id")
	delete(info, "inputs")
	value, err := fn(goja.Undefined(), runtime.ToValue(info))
	if err != nil {
		return nil, err
	}
	instances := instanceDeclarationsFromExport(value.Export())
	if len(instances) == 0 {
		return nil, errors.New("workflow instances must declare at least one node instance")
	}
	for instanceID, declaration := range instances {
		if instanceID == "" {
			return nil, errors.New("workflow instance id is required")
		}
		if declaration.Node == "" {
			return nil, fmt.Errorf("workflow instance %q node is required", instanceID)
		}
		if _, ok := runner.nodes[declaration.Node]; !ok {
			return nil, fmt.Errorf("workflow instance %q node %q is not registered", instanceID, declaration.Node)
		}
		if declaration.Params == nil {
			declaration.Params = map[string]any{}
			instances[instanceID] = declaration
		}
	}
	return instances, ctx.Err()
}

func (runner *Runner) Trigger(ctx context.Context, definition Definition) (TriggerDeclaration, error) {
	instances, err := runner.Instances(ctx, definition)
	if err != nil {
		return TriggerDeclaration{}, err
	}
	runtime := newRuntime()
	if _, err := runtime.RunString(definition.Script); err != nil {
		return TriggerDeclaration{}, err
	}
	fn, ok := goja.AssertFunction(runtime.Get("trigger"))
	if !ok {
		return TriggerDeclaration{}, ErrMissingTrigger
	}
	info := workflowInfo(definition, "", nil)
	delete(info, "run_id")
	delete(info, "inputs")
	value, err := fn(goja.Undefined(), runtime.ToValue(info))
	if err != nil {
		return TriggerDeclaration{}, err
	}
	declaration := triggerDeclarationFromExport(value.Export())
	if declaration.Instance == "" {
		return TriggerDeclaration{}, errors.New("workflow trigger must return an instance")
	}
	instance, ok := instances[declaration.Instance]
	if !ok {
		return TriggerDeclaration{}, fmt.Errorf("trigger instance %q is not declared", declaration.Instance)
	}
	declaration.Node = instance.Node
	node, ok := runner.nodes[instance.Node]
	if !ok {
		return TriggerDeclaration{}, fmt.Errorf("trigger node %q is not registered", declaration.Node)
	}
	if !node.Info().Trigger {
		return TriggerDeclaration{}, fmt.Errorf("node %q is not a trigger node", declaration.Node)
	}
	if declaration.Params == nil {
		declaration.Params = map[string]any{}
	}
	return declaration, ctx.Err()
}

func (runner *Runner) TriggerRunInputs(ctx context.Context, definition Definition, declaration TriggerDeclaration, event TriggerEvent) (map[string]any, bool, error) {
	instances, err := runner.Instances(ctx, definition)
	if err != nil {
		return nil, false, err
	}
	instance, ok := instances[declaration.Instance]
	if !ok {
		return nil, false, fmt.Errorf("trigger instance %q is not declared", declaration.Instance)
	}
	node, ok := runner.nodes[declaration.Node]
	if !ok {
		return nil, false, fmt.Errorf("trigger node %q is not registered", declaration.Node)
	}
	triggerNode, ok := node.(TriggerNode)
	if !ok {
		return nil, false, fmt.Errorf("node %q cannot run trigger events", declaration.Node)
	}
	output, matched, err := triggerNode.RunTrigger(ctx, cloneAnyMap(declaration.Params), event, RuntimeInfo{
		WorkflowID:   definition.ID,
		DatabaseName: definition.DatabaseName,
		InstanceID:   declaration.Instance,
		NodeType:     declaration.Node,
		CreatorID:    definition.CreatorID,
		Secrets:      instanceStringMap(definition.Secrets, declaration.Instance, instance.Secrets),
		Variables:    instanceStringMap(definition.Variables, declaration.Instance, instance.Variables),
	})
	if err != nil || !matched {
		return nil, matched, err
	}
	return cloneAnyMap(output), true, ctx.Err()
}

func (runner *Runner) runInstance(ctx context.Context, definition Definition, runID, instanceID string, declaration InstanceDeclaration, input map[string]any, run *history.WorkflowRun) (map[string]any, error) {
	node, ok := runner.nodes[declaration.Node]
	step := history.StepRecord{NodeID: instanceID, NodeType: declaration.Node, Input: cloneAnyMap(input)}
	if !ok {
		step.Error = "node is not registered"
		run.Steps = append(run.Steps, step)
		return nil, fmt.Errorf("node %q is not registered", declaration.Node)
	}
	output, err := node.Run(ctx, input, RuntimeInfo{
		WorkflowID:   definition.ID,
		DatabaseName: definition.DatabaseName,
		RunID:        runID,
		InstanceID:   instanceID,
		NodeType:     declaration.Node,
		CreatorID:    definition.CreatorID,
		Secrets:      instanceStringMap(definition.Secrets, instanceID, declaration.Secrets),
		Variables:    instanceStringMap(definition.Variables, instanceID, declaration.Variables),
	})
	if err != nil {
		step.Error = err.Error()
		run.Steps = append(run.Steps, step)
		return nil, err
	}
	step.Output = cloneAnyMap(output)
	run.Steps = append(run.Steps, step)
	return output, nil
}

func (runner *Runner) finish(ctx context.Context, run history.WorkflowRun, runErr error) (history.WorkflowRun, string, error) {
	if runErr != nil {
		run.Error = runErr.Error()
		run.Outputs = map[string]any{"error": runErr.Error()}
	}
	key, err := history.SaveWorkflowRun(ctx, runner.store, run)
	if err != nil {
		return run, "", err
	}
	return run, key, runErr
}

func newRuntime() *goja.Runtime {
	runtime := goja.New()
	runtime.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	return runtime
}

func workflowInfo(definition Definition, runID string, inputs map[string]any) map[string]any {
	return map[string]any{
		"workflow_id":   definition.ID,
		"database_name": definition.DatabaseName,
		"run_id":        runID,
		"inputs":        cloneAnyMap(inputs),
	}
}

func triggerDeclarationFromExport(value any) TriggerDeclaration {
	values, ok := value.(map[string]any)
	if !ok {
		return TriggerDeclaration{}
	}
	declaration := TriggerDeclaration{}
	if instance, ok := values["instance"].(string); ok {
		declaration.Instance = instance
	}
	declaration.Params = exportedMap(values["params"])
	return declaration
}

func instanceDeclarationsFromExport(value any) map[string]InstanceDeclaration {
	values, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	instances := map[string]InstanceDeclaration{}
	for instanceID, rawDeclaration := range values {
		switch declaration := rawDeclaration.(type) {
		case string:
			instances[instanceID] = InstanceDeclaration{Node: declaration}
		case map[string]any:
			instances[instanceID] = InstanceDeclaration{
				Node:      stringValue(declaration["node"]),
				Variables: portsFromExport(declaration["variables"]),
				Secrets:   portsFromExport(declaration["secrets"]),
				Params:    exportedMap(declaration["params"]),
			}
		}
	}
	return instances
}

func portsFromExport(value any) []Port {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	ports := make([]Port, 0, len(values))
	for _, value := range values {
		switch port := value.(type) {
		case string:
			ports = append(ports, Port{Name: port, Type: "string"})
		case map[string]any:
			ports = append(ports, Port{
				Name:        stringValue(port["name"]),
				Type:        stringValue(port["type"]),
				Description: stringValue(port["description"]),
			})
		}
	}
	return ports
}

func instanceStringMap(values map[string]string, instanceID string, ports []Port) map[string]string {
	prefix := instanceID + "."
	scoped := map[string]string{}
	if len(ports) == 0 {
		for key, value := range values {
			if strings.HasPrefix(key, prefix) {
				scoped[strings.TrimPrefix(key, prefix)] = value
			}
		}
		return scoped
	}
	for _, port := range ports {
		if port.Name == "" {
			continue
		}
		if value, ok := values[prefix+port.Name]; ok {
			scoped[port.Name] = value
		}
	}
	return scoped
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func exportedMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if values, ok := value.(map[string]any); ok {
		return cloneAnyMap(values)
	}
	return map[string]any{"value": value}
}

func cloneAnyMap(values map[string]any) map[string]any {
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
