package repositorysync

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"autable/internal/repository"
)

type Change struct {
	ActorID    string
	ActorLabel string
	Action     string
	Summary    string
	Paths      []string
	Time       time.Time
}

type Status struct {
	State          string `json:"state"`
	LastCommit     string `json:"last_commit,omitempty"`
	LastPushedAt   int64  `json:"last_pushed_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	PendingChanges int    `json:"pending_changes"`
}

type Options struct {
	Root        string
	RemoteURL   string
	Branch      string
	Debounce    time.Duration
	PushTimeout time.Duration
	AuthorName  string
	AuthorEmail string
}

type Service struct {
	options Options
	git     *repository.GitRepository
	changes chan Change
	done    chan struct{}
	cancel  context.CancelFunc

	mu      sync.Mutex
	status  Status
	pending []Change
}

func New(options Options) *Service {
	gitRepo, err := repository.OpenOrCloneGitRepository(context.Background(), repository.GitOptions{
		Path:         options.Root,
		RemoteURL:    options.RemoteURL,
		RemoteBranch: options.Branch,
	})
	if err != nil {
		slog.Error("repository sync git initialization failed", "error", err)
	}
	if options.Debounce <= 0 {
		options.Debounce = 2 * time.Second
	}
	if options.PushTimeout <= 0 {
		options.PushTimeout = 30 * time.Second
	}
	if strings.TrimSpace(options.AuthorName) == "" {
		options.AuthorName = "autable"
	}
	if strings.TrimSpace(options.AuthorEmail) == "" {
		options.AuthorEmail = "autable@example.local"
	}
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		options: options,
		git:     gitRepo,
		changes: make(chan Change, 256),
		done:    make(chan struct{}),
		cancel:  cancel,
		status:  Status{State: "idle"},
	}
	go service.run(ctx)
	return service
}

func (service *Service) Notify(change Change) {
	if change.Time.IsZero() {
		change.Time = time.Now().UTC()
	}
	select {
	case service.changes <- change:
	default:
		service.setFailed(errors.New("repository sync queue is full"))
	}
}

func (service *Service) Status() Status {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.status
}

func (service *Service) Shutdown(ctx context.Context) error {
	service.cancel()
	select {
	case <-service.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (service *Service) run(ctx context.Context) {
	defer close(service.done)
	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			service.flush()
			return
		case change := <-service.changes:
			service.addPending(change)
			if timer == nil {
				timer = time.NewTimer(service.options.Debounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(service.options.Debounce)
			}
			timerC = timer.C
		case <-timerC:
			timerC = nil
			timer = nil
			service.flush()
		}
	}
}

func (service *Service) addPending(change Change) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.pending = append(service.pending, change)
	service.status.State = "pending"
	service.status.LastError = ""
	service.status.PendingChanges = len(service.pending)
}

func (service *Service) flush() {
	changes := service.takePending()
	if len(changes) == 0 {
		service.setState("idle")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), service.options.PushTimeout)
	defer cancel()
	if service.git == nil {
		service.restorePending(changes)
		service.setFailed(errors.New("repository git service is not initialized"))
		return
	}

	service.setState("committing")
	commit, changed, err := service.commit(ctx, changes)
	if err != nil {
		service.restorePending(changes)
		service.setFailed(err)
		slog.Error("repository sync commit failed", "error", err)
		return
	}
	if !changed {
		service.setState("idle")
		return
	}

	service.mu.Lock()
	service.status.State = "idle"
	service.status.LastCommit = commit
	service.status.LastPushedAt = time.Now().UTC().UnixMilli()
	service.status.LastError = ""
	service.status.PendingChanges = len(service.pending)
	service.mu.Unlock()
	slog.Info("repository sync pushed", "commit", commit)
}

func (service *Service) takePending() []Change {
	service.mu.Lock()
	defer service.mu.Unlock()
	changes := append([]Change(nil), service.pending...)
	service.pending = nil
	service.status.PendingChanges = 0
	return changes
}

func (service *Service) restorePending(changes []Change) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.pending = append(changes, service.pending...)
	service.status.PendingChanges = len(service.pending)
}

func (service *Service) setState(state string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.status.State = state
	service.status.PendingChanges = len(service.pending)
}

func (service *Service) setFailed(err error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.status.State = "failed"
	service.status.LastError = service.mask(err.Error())
	service.status.PendingChanges = len(service.pending)
}

func (service *Service) commit(ctx context.Context, changes []Change) (string, bool, error) {
	paths := []string{}
	for _, path := range []string{"metadata", "workflow", "form"} {
		if _, err := os.Stat(filepath.Join(service.options.Root, path)); err == nil {
			paths = append(paths, path)
		} else if err != nil && !os.IsNotExist(err) {
			return "", false, err
		}
	}
	if len(paths) == 0 {
		return "", false, nil
	}
	message := service.commitMessage(changes)
	return service.git.CommitAndPush(ctx, repository.CommitOptions{
		Paths:       paths,
		Message:     message,
		AuthorName:  service.options.AuthorName,
		AuthorEmail: service.options.AuthorEmail,
		When:        time.Now().UTC(),
	})
}

func (service *Service) commitMessage(changes []Change) string {
	var builder strings.Builder
	builder.WriteString("autable: sync repository changes\n\nChanges:\n")
	for _, change := range changes {
		actor := change.ActorLabel
		if strings.TrimSpace(actor) == "" {
			actor = change.ActorID
		}
		if strings.TrimSpace(actor) == "" {
			actor = "unknown"
		}
		summary := change.Summary
		if strings.TrimSpace(summary) == "" {
			summary = change.Action
		}
		builder.WriteString("- ")
		builder.WriteString(actor)
		builder.WriteString(" ")
		builder.WriteString(summary)
		builder.WriteString("\n")
		paths := service.relativePaths(change.Paths)
		if len(paths) > 0 {
			builder.WriteString("  files:\n")
			for _, path := range paths {
				builder.WriteString("  - ")
				builder.WriteString(path)
				builder.WriteString("\n")
			}
		}
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (service *Service) relativePaths(paths []string) []string {
	output := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		rel := path
		if filepath.IsAbs(path) {
			if next, err := filepath.Rel(service.options.Root, path); err == nil {
				rel = next
			}
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if strings.HasPrefix(rel, "../") || rel == ".." {
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		output = append(output, rel)
	}
	return output
}

func (service *Service) mask(value string) string {
	if service.options.RemoteURL == "" {
		return value
	}
	return strings.ReplaceAll(value, service.options.RemoteURL, repository.MaskRemoteURL(service.options.RemoteURL))
}
