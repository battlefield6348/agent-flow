package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Scheduler 負責管理定期執行的排程任務，支援多 Agent 獨立輪詢
type Scheduler struct {
	service          *OrchestratorService
	interval         time.Duration
	allowedProjects  []string
	allowedMRAuthors []string
	collaborators    []CollaboratorConfig
	gitlabURL        string
	mu               sync.Mutex
	ctx              context.Context
	agentCancels     map[string]context.CancelFunc
}

func NewScheduler(service *OrchestratorService, interval time.Duration, allowedProjects, allowedAuthors []string, collaborators []CollaboratorConfig, gitlabURL string) *Scheduler {
	return &Scheduler{
		service:          service,
		interval:         interval,
		allowedProjects:  allowedProjects,
		allowedMRAuthors: allowedAuthors,
		collaborators:    collaborators,
		gitlabURL:        gitlabURL,
		agentCancels:     make(map[string]context.CancelFunc),
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("Starting multi-agent background scheduler", "agent_count", len(s.collaborators))
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	for _, col := range s.collaborators {
		if err := s.StartAgent(col); err != nil {
			slog.Error("Failed to start agent polling", "agent_id", col.ID, "error", err)
		}
	}
}

func (s *Scheduler) StartAgent(col CollaboratorConfig) error {
	s.mu.Lock()
	if s.ctx == nil {
		s.mu.Unlock()
		return fmt.Errorf("scheduler is not started")
	}
	if _, exists := s.agentCancels[col.ID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("agent %s is already scheduled", col.ID)
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.agentCancels[col.ID] = cancel
	s.mu.Unlock()
	if s.service != nil {
		go s.startPollingForAgent(ctx, col)
	}
	return nil
}

func (s *Scheduler) Update(settings WorkflowSettings) {
	s.mu.Lock()
	s.interval = time.Duration(settings.IntervalSeconds) * time.Second
	if s.interval <= 0 {
		s.interval = time.Minute
	}
	s.allowedProjects = settings.AllowedProjects
	s.allowedMRAuthors = settings.AllowedMRAuthors
	s.gitlabURL = settings.GitLabURL
	s.mu.Unlock()
}

func (s *Scheduler) startPollingForAgent(ctx context.Context, col CollaboratorConfig) {
	slog.Info("Starting polling loop for agent", "agent_id", col.ID, "interval", s.interval)

	repo := NewHttpGitLabRepository(s.gitlabURL, col.GitLabToken)
	if username, err := repo.GetUsername(ctx); err == nil {
		slog.Info("Detected GitLab user for agent", "agent_id", col.ID, "username", username)
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		err := s.service.ScanAndAssignForAgent(ctx, col.ID, repo, s.allowedProjects, s.allowedMRAuthors)
		if err != nil {
			slog.Error("Error during GitLab scan for agent", "agent_id", col.ID, "error", err)
		}

		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			slog.Info("Stopping polling loop for agent", "agent_id", col.ID)
			return
		}
	}
}
