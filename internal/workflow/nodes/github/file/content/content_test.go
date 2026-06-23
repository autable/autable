package content

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"autable/internal/workflow"

	"github.com/google/go-github/v88/github"
)

type fakeGitHubContentClient struct {
	owner     string
	repo      string
	path      string
	ref       string
	file      *github.RepositoryContent
	directory []*github.RepositoryContent
	response  *github.Response
	err       error
}

func (client *fakeGitHubContentClient) GetContents(_ context.Context, owner string, repo string, path string, opts *github.RepositoryContentGetOptions) (*github.RepositoryContent, []*github.RepositoryContent, *github.Response, error) {
	client.owner = owner
	client.repo = repo
	client.path = path
	if opts != nil {
		client.ref = opts.Ref
	}
	return client.file, client.directory, client.response, client.err
}

func TestGitHubFileContentNodeGetsDecodedFileContent(t *testing.T) {
	fake := &fakeGitHubContentClient{
		file: &github.RepositoryContent{
			Type:        github.Ptr("file"),
			Encoding:    github.Ptr("base64"),
			Size:        github.Ptr(17),
			Name:        github.Ptr("README.md"),
			Path:        github.Ptr("docs/README.md"),
			Content:     github.Ptr(base64.StdEncoding.EncodeToString([]byte("# Autable\n"))),
			SHA:         github.Ptr("sha-1"),
			HTMLURL:     github.Ptr("https://github.com/autable/autable/blob/main/docs/README.md"),
			DownloadURL: github.Ptr("https://raw.githubusercontent.com/autable/autable/main/docs/README.md"),
		},
		response: githubResponse(200),
	}
	var usedToken string
	node := NewNodeForTest(func(token string) (githubContentClient, error) {
		usedToken = token
		return fake, nil
	})
	output, err := node.Run(context.Background(), map[string]any{
		"owner": "autable",
		"repo":  "autable",
		"path":  "docs/README.md",
		"ref":   "main",
	}, workflow.RuntimeInfo{Secrets: map[string]string{"token": " github-token "}})
	if err != nil {
		t.Fatal(err)
	}
	if usedToken != "github-token" {
		t.Fatalf("unexpected token: %q", usedToken)
	}
	if fake.owner != "autable" || fake.repo != "autable" || fake.path != "docs/README.md" || fake.ref != "main" {
		t.Fatalf("unexpected request: %#v", fake)
	}
	if output["content"] != "# Autable\n" || output["status_code"] != 200 || output["sha"] != "sha-1" {
		t.Fatalf("unexpected output: %#v", output)
	}
	if outputContains(output, "github-token") {
		t.Fatalf("node output leaked secret values: %#v", output)
	}
}

func TestGitHubFileContentNodeAllowsPublicRepositoryWithoutToken(t *testing.T) {
	fake := &fakeGitHubContentClient{
		file: &github.RepositoryContent{
			Type:     github.Ptr("file"),
			Encoding: github.Ptr(""),
			Content:  github.Ptr("plain text"),
		},
	}
	var usedToken string
	node := NewNodeForTest(func(token string) (githubContentClient, error) {
		usedToken = token
		return fake, nil
	})
	output, err := node.Run(context.Background(), map[string]any{
		"owner": "autable",
		"repo":  "autable",
		"path":  "README.md",
	}, workflow.RuntimeInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if usedToken != "" {
		t.Fatalf("expected empty token, got %q", usedToken)
	}
	if output["content"] != "plain text" {
		t.Fatalf("unexpected content: %#v", output)
	}
}

func TestGitHubFileContentNodeRejectsMissingInputsAndDirectory(t *testing.T) {
	node := NewNodeForTest(func(string) (githubContentClient, error) { return &fakeGitHubContentClient{}, nil })
	if _, err := node.Run(context.Background(), map[string]any{"repo": "autable", "path": "README.md"}, workflow.RuntimeInfo{}); err == nil {
		t.Fatal("expected missing owner error")
	}
	if _, err := node.Run(context.Background(), map[string]any{"owner": "autable", "path": "README.md"}, workflow.RuntimeInfo{}); err == nil {
		t.Fatal("expected missing repo error")
	}
	if _, err := node.Run(context.Background(), map[string]any{"owner": "autable", "repo": "autable"}, workflow.RuntimeInfo{}); err == nil {
		t.Fatal("expected missing path error")
	}

	directoryNode := NewNodeForTest(func(string) (githubContentClient, error) {
		return &fakeGitHubContentClient{directory: []*github.RepositoryContent{{Type: github.Ptr("file")}}}, nil
	})
	if _, err := directoryNode.Run(context.Background(), map[string]any{"owner": "autable", "repo": "autable", "path": "docs"}, workflow.RuntimeInfo{}); err == nil {
		t.Fatal("expected directory error")
	}

	apiErr := errors.New("github failed")
	errorNode := NewNodeForTest(func(string) (githubContentClient, error) {
		return &fakeGitHubContentClient{response: githubResponse(404), err: apiErr}, nil
	})
	output, err := errorNode.Run(context.Background(), map[string]any{"owner": "autable", "repo": "autable", "path": "missing.md"}, workflow.RuntimeInfo{})
	if !errors.Is(err, apiErr) || output["status_code"] != 404 {
		t.Fatalf("expected api error with status output, got output=%#v err=%v", output, err)
	}

	clientErr := errors.New("client failed")
	clientErrorNode := NewNodeForTest(func(string) (githubContentClient, error) {
		return nil, clientErr
	})
	if _, err := clientErrorNode.Run(context.Background(), map[string]any{"owner": "autable", "repo": "autable", "path": "README.md"}, workflow.RuntimeInfo{}); !errors.Is(err, clientErr) {
		t.Fatalf("expected client error, got %v", err)
	}
}

func TestGitHubFileContentNodeIsAvailableInNodeInfos(t *testing.T) {
	runner := workflow.NewRunner(nil, NewNodeForTest(func(string) (githubContentClient, error) {
		return &fakeGitHubContentClient{}, nil
	}))
	infos := runner.NodeInfos()
	if len(infos) != 1 || infos[0].Type != "github.file.content.get" {
		t.Fatalf("expected github file content node info, got %#v", infos)
	}
	if len(infos[0].Inputs) != 4 || infos[0].Inputs[0].Name != "owner" {
		t.Fatalf("expected github input metadata, got %#v", infos[0].Inputs)
	}
	if len(infos[0].Secrets) != 1 || infos[0].Secrets[0].Name != "token" {
		t.Fatalf("expected optional token secret metadata, got %#v", infos[0].Secrets)
	}
	if infos[0].Documentation["en-US"] == "" || infos[0].Documentation["zh-CN"] == "" {
		t.Fatalf("expected embedded documentation, got %#v", infos[0].Documentation)
	}
}

func outputContains(value map[string]any, text string) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return strings.Contains(string(encoded), text)
}

func githubResponse(statusCode int) *github.Response {
	return &github.Response{Response: &http.Response{StatusCode: statusCode}}
}
