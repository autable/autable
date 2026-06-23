package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestEnsureGitRepositoryClonesMissingPath(t *testing.T) {
	ctx := context.Background()
	remote := newBareRemote(t)
	target := filepath.Join(t.TempDir(), "repository")

	if err := EnsureGitRepository(ctx, GitOptions{
		Path:         target,
		RemoteURL:    remote,
		RemoteBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Fatalf("expected cloned git repository: %v", err)
	}
	head, err := os.ReadFile(filepath.Join(target, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(head)) != "ref: refs/heads/main" {
		t.Fatalf("expected main HEAD, got %q", head)
	}
}

func TestEnsureGitRepositorySupportsEmptyRemote(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "empty.git")
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "repository")
	if err := EnsureGitRepository(ctx, GitOptions{
		Path:         target,
		RemoteURL:    remote,
		RemoteBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	head, err := os.ReadFile(filepath.Join(target, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(head)) != "ref: refs/heads/main" {
		t.Fatalf("expected empty clone HEAD to target main, got %q", head)
	}
}

func TestCommitAndPushPushesExistingAheadCommitWithoutNewChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "empty.git")
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "repository")
	repo, err := OpenOrCloneGitRepository(ctx, GitOptions{Path: target, RemoteURL: remote, RemoteBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "metadata", "main.yml"), []byte("databases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.AddWithOptions(&git.AddOptions{All: true, Path: "metadata"}); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("local only", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	commit, changed, err := repo.CommitAndPush(ctx, CommitOptions{
		Paths:       []string{"metadata"},
		Message:     "retry push",
		AuthorName:  "Test",
		AuthorEmail: "test@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !changed || commit == "" {
		t.Fatalf("expected retry push to report pushed commit, got changed=%v commit=%q", changed, commit)
	}
	remoteRepo, err := git.PlainOpen(remote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remoteRepo.Reference(plumbing.NewBranchReferenceName("main"), true); err != nil {
		t.Fatalf("expected remote main branch after retry push: %v", err)
	}
}

func TestCommitAndPushIgnoresUntrackedFilesOutsideManagedPaths(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "empty.git")
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "repository")
	repo, err := OpenOrCloneGitRepository(ctx, GitOptions{Path: target, RemoteURL: remote, RemoteBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "metadata", "main.yml"), []byte("databases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := repo.CommitAndPush(ctx, CommitOptions{
		Paths:       []string{"metadata"},
		Message:     "initial",
		AuthorName:  "Test",
		AuthorEmail: "test@example.com",
	}); err != nil || !changed {
		t.Fatalf("expected initial push, changed=%v err=%v", changed, err)
	}
	if err := os.WriteFile(filepath.Join(target, "config.yml"), []byte("secret: value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit, changed, err := repo.CommitAndPush(ctx, CommitOptions{
		Paths:       []string{"metadata"},
		Message:     "no managed changes",
		AuthorName:  "Test",
		AuthorEmail: "test@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed || commit != "" {
		t.Fatalf("expected no managed changes, got changed=%v commit=%q", changed, commit)
	}
}

func TestMaskRemoteURL(t *testing.T) {
	got := MaskRemoteURL("https://token@example.com/org/repo.git")
	if got != "https://***@example.com/org/repo.git" {
		t.Fatalf("unexpected masked URL: %q", got)
	}
}

func newBareRemote(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainInitWithOptions(work, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{remote}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("seed", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.com", When: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RemoteURL:  remote,
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/main:refs/heads/main"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	return remote
}
