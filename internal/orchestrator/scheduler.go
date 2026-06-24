package orchestrator

import (
	"context"
	"log/slog"
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
}

func NewScheduler(service *OrchestratorService, interval time.Duration, allowedProjects, allowedAuthors []string, collaborators []CollaboratorConfig, gitlabURL string) *Scheduler {
	return &Scheduler{
		service:          service,
		interval:         interval,
		allowedProjects:  allowedProjects,
		allowedMRAuthors: allowedAuthors,
		collaborators:    collaborators,
		gitlabURL:        gitlabURL,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("Starting multi-agent background scheduler", "agent_count", len(s.collaborators))

	// 初始等待，確保 Worker 有時間初始化
	time.Sleep(15 * time.Second)

	for _, col := range s.collaborators {
		go s.startPollingForAgent(ctx, col)
	}

	<-ctx.Done()
	slog.Info("Stopping background scheduler")
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
