# Repository Rules

- Release versions are represented by Git tags only.
- A normal push to `main` creates the next patch `vX.Y.Z` tag and a GitHub Release without committing version changes back to `main`.
- If no release tag exists yet, use `web/package.json` as the initial semantic-version baseline without editing it.
- Manual release workflow runs may create `patch`, `minor`, or `major` tags; use `patch` unless the user explicitly asks for a different release level.
- Do not add version bump commits, generated release commits, or package version edits to application branches.
- Do not manually create release tags unless the user explicitly asks for a manual release fix.
- Release binaries must embed the built frontend by copying `web/dist` into `internal/webui/dist` before `go build`.
- Release artifacts are built for Linux amd64/arm64, Windows amd64, and macOS arm64 only.
- CI uses Node.js 24 and the latest stable Go compiler; keep both current when release tooling changes.
- Before introducing any library, SDK, framework, or development tool, use network access to verify the latest official version from the project's official source, package registry, or primary documentation. Do not rely on memory for dependency versions.
