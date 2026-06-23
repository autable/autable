## GitHub file content

Reads a single file from a GitHub repository through the official `go-github` SDK.

### Secrets

- `token` (`string`): Optional GitHub token. Use it for private repositories or higher rate limits.

### Inputs

- `owner` (`string`): Repository owner or organization.
- `repo` (`string`): Repository name.
- `path` (`string`): File path in the repository.
- `ref` (`string`): Optional branch, tag, or commit SHA.

### Outputs

- `content` (`string`): Decoded file content.
- `name` (`string`): File name.
- `path` (`string`): File path.
- `sha` (`string`): Git blob SHA.
- `size` (`int`): File size.
- `encoding` (`string`): GitHub response encoding.
- `type` (`string`): Content type.
- `html_url` (`string`): GitHub web URL.
- `download_url` (`string`): Raw file download URL.
- `status_code` (`int`): GitHub API status code when available.

### Example

```js
function instances(info) {
  return {
    read_readme: {
      node: "github.file.content.get",
      secrets: [{ name: "token", type: "string" }]
    }
  };
}

function run(info) {
  return info.instance("read_readme").exec({
    owner: "autable",
    repo: "autable",
    path: "README.md",
    ref: "main"
  });
}
```
