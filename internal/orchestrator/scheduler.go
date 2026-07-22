package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
	gitlabToken      string
	mu               sync.Mutex
	ctx              context.Context
	agentCancels     map[string]context.CancelFunc
}

func NewScheduler(service *OrchestratorService, interval time.Duration, allowedProjects, allowedAuthors []string, collaborators []CollaboratorConfig, gitlabURL, gitlabToken string) *Scheduler {
	if collaborators == nil {
		collaborators = []CollaboratorConfig{
			{ID: "reviewer"},
			{ID: "coder"},
		}
	}
	return &Scheduler{
		service:          service,
		interval:         interval,
		allowedProjects:  allowedProjects,
		allowedMRAuthors: allowedAuthors,
		collaborators:    collaborators,
		gitlabURL:        gitlabURL,
		gitlabToken:      gitlabToken,
		agentCancels:     make(map[string]context.CancelFunc),
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
	slog.Info("啟動多 Agent 背景輪詢服務", "agent_count", len(s.collaborators), "interval", s.interval)
	for _, col := range s.collaborators {
		if err := s.StartAgent(col); err != nil {
			slog.Error("啟動 Agent 輪詢失敗", "agent_id", col.ID, "error", err)
		}
	}
}

func (s *Scheduler) StartAgent(col CollaboratorConfig) error {
	s.mu.Lock()
	if s.ctx == nil {
		s.mu.Unlock()
		return fmt.Errorf("scheduler 未啟動")
	}
	if _, exists := s.agentCancels[col.ID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("agent %s 已在排程中", col.ID)
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.agentCancels[col.ID] = cancel
	s.mu.Unlock()
	if s.service != nil {
		go s.startPollingForAgent(ctx, col)
	}
	return nil
}

func (s *Scheduler) StopAgent(id string) error {
	s.mu.Lock()
	cancel, exists := s.agentCancels[id]
	if exists {
		delete(s.agentCancels, id)
	}
	s.mu.Unlock()
	if !exists {
		return fmt.Errorf("agent %s 不在排程中", id)
	}
	cancel()
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
	s.gitlabToken = settings.GitLabToken
	s.mu.Unlock()
	if s.service != nil {
		s.service.SetCheckCISuccess(settings.CheckCISuccess)
	}
}

func (s *Scheduler) startPollingForAgent(ctx context.Context, col CollaboratorConfig) {
	slog.Info("開始執行 Agent 輪詢迴圈", "agent_id", col.ID, "interval", s.interval)

	token := col.GitLabToken
	if token == "" {
		token = s.gitlabToken
	}
	if token == "" {
		token = os.Getenv("GITLAB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GITLAB_PRIVATE_TOKEN")
	}

	repo := NewHttpGitLabRepository(s.gitlabURL, token)
	if username, err := repo.GetUsername(ctx); err == nil {
		slog.Info("已偵測到 GitLab 帳號", "agent_id", col.ID, "username", username)
	} else {
		slog.Warn("無法取得 GitLab 帳號資訊 (請檢查 gitlab_token)", "agent_id", col.ID, "error", err)
	}

	// 啟動時立即執行一次掃描，無需等待第一個 ticker 到期
	s.executeScan(ctx, col, repo)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.executeScan(ctx, col, repo)
		case <-ctx.Done():
			slog.Info("停止 Agent 輪詢迴圈", "agent_id", col.ID)
			return
		}
	}
}

func (s *Scheduler) executeScan(ctx context.Context, col CollaboratorConfig, repo GitLabRepository) {
	slog.Info("正在輪詢掃描 GitLab Todos...", "agent_id", col.ID)
	err := s.service.ScanAndAssignForAgent(ctx, col.ID, repo, s.allowedProjects, s.allowedMRAuthors, col.CaoSessionName)
	if err != nil {
		slog.Error("掃描 GitLab Todos 發生錯誤", "agent_id", col.ID, "error", err)
	}
}
