# Node Development Notes

- Put each workflow node in its own directory under `internal/workflow/nodes`. Multi-level directories are preferred for families such as `dingtalk/...` and `table/...`.
- Keep each node's documentation in that node directory, embedded by the node package, and expose it through a package-level `Documentation()` method used by `Info()`.
- Keep node implementations thin: parse node-local inputs, describe ports, and delegate host application capabilities to injected services. Do not make node packages depend on `internal/api` or the main `Server`.
- Register new built-in nodes in `internal/workflow/nodes/registry.go`. Adding a node should usually mean adding its node package plus one list entry.
- Use `internal/workflow/nodes/autable.Service` for Autable capabilities exposed to nodes. Add methods there when a node needs a new Autable API, then implement them in the API service adapter.
- Use `github.com/alibabacloud-go/dingtalk` for DingTalk OpenAPI integrations. Do not hand-roll raw HTTP calls for DingTalk OpenAPI endpoints; wrap the SDK behind small node-local interfaces when tests need fakes.
- Keep workflow node inputs and outputs plain JSON-compatible values so workflow scripts do not depend on SDK types.
- Do not add fallback behavior for paths that should fail. If ordered metadata, configured fields, views, or service calls are invalid, return the error instead of silently falling back to another order or data source.
