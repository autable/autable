package content

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"autable/internal/workflow"

	"github.com/google/go-github/v88/github"
)

type githubContentClient interface {
	GetContents(ctx context.Context, owner string, repo string, path string, opts *github.RepositoryContentGetOptions) (*github.RepositoryContent, []*github.RepositoryContent, *github.Response, error)
}

type Node struct {
	client func(token string) (githubContentClient, error)
}

func NewNode() Node {
	return Node{
		client: func(token string) (githubContentClient, error) {
			opts := []github.ClientOptionsFunc{}
			if token != "" {
				opts = append(opts, github.WithAuthToken(token))
			}
			client, err := github.NewClient(opts...)
			if err != nil {
				return nil, err
			}
			return client.Repositories, nil
		},
	}
}

func NewNodeForTest(client func(token string) (githubContentClient, error)) Node {
	return Node{client: client}
}

func (node Node) Info() workflow.NodeInfo {
	return workflow.NodeInfo{
		Type:          "github.file.content.get",
		DisplayName:   "GitHub file content",
		Description:   "Reads a file from a GitHub repository through the official go-github SDK.",
		Documentation: Documentation(),
		Inputs: []workflow.Port{
			{Name: "owner", Type: "string", Description: "Repository owner or organization."},
			{Name: "repo", Type: "string", Description: "Repository name."},
			{Name: "path", Type: "string", Description: "File path in the repository."},
			{Name: "ref", Type: "string", Description: "Optional branch, tag, or commit SHA."},
		},
		Outputs: []workflow.Port{
			{Name: "content", Type: "string", Description: "Decoded file content."},
			{Name: "name", Type: "string"},
			{Name: "path", Type: "string"},
			{Name: "sha", Type: "string"},
			{Name: "size", Type: "int"},
			{Name: "encoding", Type: "string"},
			{Name: "type", Type: "string"},
			{Name: "html_url", Type: "string"},
			{Name: "download_url", Type: "string"},
			{Name: "status_code", Type: "int"},
		},
		Secrets: []workflow.Port{
			{Name: "token", Type: "string", Description: "Optional GitHub token for private repositories or higher rate limits."},
		},
		Stateless: true,
	}
}

func (node Node) Run(ctx context.Context, input map[string]any, info workflow.RuntimeInfo) (map[string]any, error) {
	clientFactory := node.client
	if clientFactory == nil {
		clientFactory = NewNode().client
	}
	owner := strings.TrimSpace(stringInput(input, "owner"))
	if owner == "" {
		return nil, errors.New("github owner is required")
	}
	repo := strings.TrimSpace(stringInput(input, "repo"))
	if repo == "" {
		return nil, errors.New("github repo is required")
	}
	path := strings.TrimSpace(stringInput(input, "path"))
	if path == "" {
		return nil, errors.New("github path is required")
	}

	opts := &github.RepositoryContentGetOptions{Ref: strings.TrimSpace(stringInput(input, "ref"))}
	if opts.Ref == "" {
		opts = nil
	}
	token := strings.TrimSpace(info.Secrets["token"])
	client, err := clientFactory(token)
	if err != nil {
		return nil, err
	}
	file, directory, response, err := client.GetContents(ctx, owner, repo, path, opts)
	output := contentOutput(file, response)
	if err != nil {
		return output, err
	}
	if len(directory) > 0 {
		return output, fmt.Errorf("github path %q is a directory; file path is required", path)
	}
	if file == nil {
		return output, fmt.Errorf("github file %q was not returned", path)
	}
	content, err := file.GetContent()
	if err != nil {
		return output, err
	}
	output["content"] = content
	return output, nil
}

func contentOutput(file *github.RepositoryContent, response *github.Response) map[string]any {
	output := map[string]any{}
	if response != nil && response.Response != nil {
		output["status_code"] = response.Response.StatusCode
	}
	if file == nil {
		return output
	}
	output["name"] = file.GetName()
	output["path"] = file.GetPath()
	output["sha"] = file.GetSHA()
	output["size"] = file.GetSize()
	output["encoding"] = file.GetEncoding()
	output["type"] = file.GetType()
	output["html_url"] = file.GetHTMLURL()
	output["download_url"] = file.GetDownloadURL()
	return output
}

func stringInput(input map[string]any, key string) string {
	if value, ok := input[key].(string); ok {
		return value
	}
	return ""
}

var _ workflow.Node = Node{}
