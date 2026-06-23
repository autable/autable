package repositorysync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestServiceCommitsAndPushesChangesWithUserSummaries(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatal(err)
	}
	repoPath := filepath.Join(root, "repo")

	service := New(Options{
		Root:        repoPath,
		RemoteURL:   remote,
		Branch:      "main",
		Debounce:    10 * time.Millisecond,
		PushTimeout: 5 * time.Second,
		AuthorName:  "autable",
		AuthorEmail: "autable@example.local",
	})
	if err := os.MkdirAll(filepath.Join(repoPath, "metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, "metadata", "main.yml"), []byte("databases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Fatal(err)
		}
	}()
	service.Notify(Change{
		ActorID: "user-1",
		Summary: "updated table metadata db/contacts",
		Paths:   []string{filepath.Join(repoPath, "metadata", "main.yml")},
	})
	waitForCommit(t, service)

	local, err := git.PlainOpen(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	head, err := local.Head()
	if err != nil {
		t.Fatal(err)
	}
	commit, err := local.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(commit.Message, "user-1 updated table metadata db/contacts") {
		t.Fatalf("expected commit message to include user summary, got:\n%s", commit.Message)
	}
	if !strings.Contains(commit.Message, "metadata/main.yml") {
		t.Fatalf("expected commit message to include changed file, got:\n%s", commit.Message)
	}

	remoteRepo, err := git.PlainOpen(remote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remoteRepo.Reference(plumbing.NewBranchReferenceName("main"), true); err != nil {
		t.Fatalf("expected pushed main branch: %v", err)
	}
}

func waitForCommit(t *testing.T, service *Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status := service.Status()
		if status.LastCommit != "" && status.State == "idle" {
			return
		}
		if status.State == "failed" {
			t.Fatalf("repository sync failed: %s", status.LastError)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for repository sync, status=%#v", service.Status())
}
