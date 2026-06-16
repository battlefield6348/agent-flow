package orchestrator

import (
	"context"
	"log/slog"
	"time"
)

// Scheduler 負責管理定期執行的排程任務
type Scheduler struct {
	service          *OrchestratorService
	interval         time.Duration
	allowedProjects  []string
	allowedMRAuthors []string
}

func NewScheduler(service *OrchestratorService, interval time.Duration, allowedProjects, allowedAuthors []string) *Scheduler {
	return &Scheduler{
		service:          service,
		interval:         interval,
		allowedProjects:  allowedProjects,
		allowedMRAuthors: allowedAuthors,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("Starting background scheduler loop", "interval", s.interval)

	// 初始等待，確保 Worker 有時間初始化
	time.Sleep(15 * time.Second)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		err := s.service.ScanAndAssign(ctx, s.allowedProjects, s.allowedMRAuthors)
		if err != nil {
			slog.Error("Error during GitLab scan", "error", err)
		}

		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			slog.Info("Stopping background scheduler")
			return
		}
	}
}
