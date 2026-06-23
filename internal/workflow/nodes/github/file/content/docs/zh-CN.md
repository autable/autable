## GitHub 文件内容

通过官方 `go-github` SDK 读取 GitHub 仓库中的单个文件。

### Secrets

- `token` (`string`)：可选 GitHub token。访问私有仓库或提高限流额度时使用。

### Inputs

- `owner` (`string`)：仓库所属用户或组织。
- `repo` (`string`)：仓库名。
- `path` (`string`)：仓库内文件路径。
- `ref` (`string`)：可选分支、标签或 commit SHA。

### Outputs

- `content` (`string`)：解码后的文件内容。
- `name` (`string`)：文件名。
- `path` (`string`)：文件路径。
- `sha` (`string`)：Git blob SHA。
- `size` (`int`)：文件大小。
- `encoding` (`string`)：GitHub 响应编码。
- `type` (`string`)：内容类型。
- `html_url` (`string`)：GitHub 网页 URL。
- `download_url` (`string`)：原始文件下载 URL。
- `status_code` (`int`)：可用时返回 GitHub API 状态码。

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
