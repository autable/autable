package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

type GitOptions struct {
	Path         string
	RemoteURL    string
	RemoteBranch string
}

type GitRepository struct {
	path         string
	remoteURL    string
	remoteBranch string
	auth         transport.AuthMethod
	repo         *git.Repository
}

type CommitOptions struct {
	Paths       []string
	Message     string
	AuthorName  string
	AuthorEmail string
	When        time.Time
}

func EnsureGitRepository(ctx context.Context, options GitOptions) error {
	_, err := OpenOrCloneGitRepository(ctx, options)
	return err
}

func OpenOrCloneGitRepository(ctx context.Context, options GitOptions) (*GitRepository, error) {
	if strings.TrimSpace(options.Path) == "" {
		return nil, errors.New("repository path is required")
	}
	if strings.TrimSpace(options.RemoteURL) == "" {
		return nil, errors.New("repository remote_url is required")
	}
	if strings.TrimSpace(options.RemoteBranch) == "" {
		return nil, errors.New("repository remote_branch is required")
	}
	branchRef := plumbing.NewBranchReferenceName(options.RemoteBranch)
	if err := branchRef.Validate(); err != nil {
		return nil, fmt.Errorf("repository remote_branch %q is invalid: %w", options.RemoteBranch, err)
	}

	cleanURL := RemoteURLWithoutCredentials(options.RemoteURL)
	auth := AuthFromRemoteURL(options.RemoteURL)
	info, err := os.Stat(options.Path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(options.Path), 0o755); err != nil {
			return nil, err
		}
		repo, err := git.PlainCloneContext(ctx, options.Path, false, &git.CloneOptions{
			URL:           cleanURL,
			Auth:          auth,
			RemoteName:    "origin",
			ReferenceName: branchRef,
			SingleBranch:  true,
		})
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			if removeErr := os.RemoveAll(options.Path); removeErr != nil {
				return nil, removeErr
			}
			repo, err = git.PlainInitWithOptions(options.Path, &git.PlainInitOptions{
				InitOptions: git.InitOptions{DefaultBranch: branchRef},
			})
			if err == nil {
				err = setOrigin(repo, cleanURL)
			}
		}
		if err != nil {
			return nil, maskGitError(err, options.RemoteURL)
		}
		gitRepo := &GitRepository{path: options.Path, remoteURL: cleanURL, remoteBranch: options.RemoteBranch, auth: auth, repo: repo}
		if err := gitRepo.ensureBranch(); err != nil {
			return nil, maskGitError(err, options.RemoteURL)
		}
		return gitRepo, nil
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository path %q is not a directory", options.Path)
	}
	repo, err := git.PlainOpen(options.Path)
	if err != nil {
		return nil, fmt.Errorf("repository path %q is not a git worktree: %w", options.Path, err)
	}
	if err := setOrigin(repo, cleanURL); err != nil {
		return nil, err
	}
	gitRepo := &GitRepository{path: options.Path, remoteURL: cleanURL, remoteBranch: options.RemoteBranch, auth: auth, repo: repo}
	if err := gitRepo.ensureBranch(); err != nil {
		return nil, maskGitError(err, options.RemoteURL)
	}
	return gitRepo, nil
}

func (repo *GitRepository) CommitAndPush(ctx context.Context, options CommitOptions) (string, bool, error) {
	if options.When.IsZero() {
		options.When = time.Now().UTC()
	}
	worktree, err := repo.repo.Worktree()
	if err != nil {
		return "", false, err
	}
	for _, path := range options.Paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := worktree.AddWithOptions(&git.AddOptions{All: true, Path: path}); err != nil {
			return "", false, err
		}
	}
	status, err := worktree.Status()
	if err != nil {
		return "", false, err
	}
	var hash plumbing.Hash
	createdCommit := false
	if managedPathsStaged(status, options.Paths) {
		hash, err = worktree.Commit(options.Message, &git.CommitOptions{
			Author: &object.Signature{
				Name:  options.AuthorName,
				Email: options.AuthorEmail,
				When:  options.When,
			},
			Committer: &object.Signature{
				Name:  options.AuthorName,
				Email: options.AuthorEmail,
				When:  options.When,
			},
		})
		if errors.Is(err, git.ErrEmptyCommit) {
			createdCommit = false
		} else if err != nil {
			return "", false, err
		} else {
			createdCommit = true
		}
	}
	pushErr := repo.repo.PushContext(ctx, &git.PushOptions{
		RemoteName: "origin",
		RemoteURL:  repo.remoteURL,
		Auth:       repo.auth,
		RefSpecs: []config.RefSpec{
			config.RefSpec("refs/heads/" + repo.remoteBranch + ":refs/heads/" + repo.remoteBranch),
		},
	})
	if errors.Is(pushErr, git.NoErrAlreadyUpToDate) {
		if !createdCommit {
			return "", false, nil
		}
		return hash.String()[:7], true, nil
	}
	if pushErr != nil {
		return "", false, maskGitError(pushErr, repo.remoteURL)
	}
	if createdCommit {
		return hash.String()[:7], true, nil
	}
	head, err := repo.repo.Head()
	if err != nil {
		return "", false, nil
	}
	return head.Hash().String()[:7], true, nil
}

func managedPathsStaged(status git.Status, paths []string) bool {
	for filePath, fileStatus := range status {
		if fileStatus.Staging == git.Unmodified {
			continue
		}
		for _, managedPath := range paths {
			managedPath = filepath.ToSlash(filepath.Clean(strings.TrimSpace(managedPath)))
			if managedPath == "." || managedPath == "" {
				continue
			}
			if filePath == managedPath || strings.HasPrefix(filePath, managedPath+"/") {
				return true
			}
		}
	}
	return false
}

func RemoteURLWithoutCredentials(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

func AuthFromRemoteURL(raw string) transport.AuthMethod {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return nil
	}
	username := parsed.User.Username()
	password, hasPassword := parsed.User.Password()
	if !hasPassword {
		password = username
		username = "x-access-token"
	}
	if username == "" || password == "" {
		return nil
	}
	return &githttp.BasicAuth{Username: username, Password: password}
}

func MaskRemoteURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = nil
	withoutUser := parsed.String()
	prefix := parsed.Scheme + "://"
	if strings.HasPrefix(withoutUser, prefix) {
		return prefix + "***@" + strings.TrimPrefix(withoutUser, prefix)
	}
	return withoutUser
}

func (repo *GitRepository) ensureBranch() error {
	branchRef := plumbing.NewBranchReferenceName(repo.remoteBranch)
	worktree, err := repo.repo.Worktree()
	if err != nil {
		return err
	}
	if _, err := repo.repo.Reference(branchRef, true); err == nil {
		return worktree.Checkout(&git.CheckoutOptions{Branch: branchRef})
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return err
	}
	if _, err := repo.repo.Head(); errors.Is(err, plumbing.ErrReferenceNotFound) {
		return repo.repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, branchRef))
	} else if err != nil {
		return err
	}
	remoteRef := plumbing.NewRemoteReferenceName("origin", repo.remoteBranch)
	ref, err := repo.repo.Reference(remoteRef, true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return fmt.Errorf("repository branch %q must exist on a non-empty remote repository", repo.remoteBranch)
		}
		return err
	}
	if err := worktree.Checkout(&git.CheckoutOptions{Branch: branchRef, Hash: ref.Hash(), Create: true}); err != nil {
		return err
	}
	return repo.repo.CreateBranch(&config.Branch{
		Name:   repo.remoteBranch,
		Remote: "origin",
		Merge:  branchRef,
	})
}

func setOrigin(repo *git.Repository, remoteURL string) error {
	cfg, err := repo.Config()
	if err != nil {
		return err
	}
	if cfg.Remotes == nil {
		cfg.Remotes = map[string]*config.RemoteConfig{}
	}
	cfg.Remotes["origin"] = &config.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteURL},
		Fetch: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	}
	return repo.SetConfig(cfg)
}

func maskGitError(err error, remoteURL string) error {
	if err == nil || remoteURL == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), remoteURL, MaskRemoteURL(remoteURL)))
}
