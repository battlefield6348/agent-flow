package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// GitLabRepository 定義與 GitLab 交互的介面 (Port)
type GitLabRepository interface {
	FetchPendingTodos(ctx context.Context) ([]Todo, error)
	MarkTodoAsDone(ctx context.Context, todoID int) error
	GetUsername(ctx context.Context) (string, error)
}

// WorkspaceRepository 定義本地工作區管理的介面 (Port)
type WorkspaceRepository interface {
	FindLocalPath(ctx context.Context, projectPath string) (string, error)
}

// OrchestratorService 負責協調任務排程的業務邏輯 (Use Case)
type OrchestratorService struct {
	gitlabRepo    GitLabRepository
	workspaceRepo WorkspaceRepository
	workerManager *WorkerManager
}

func NewOrchestratorService(gl GitLabRepository, ws WorkspaceRepository, wm *WorkerManager) *OrchestratorService {
	return &OrchestratorService{
		gitlabRepo:    gl,
		workspaceRepo: ws,
		workerManager: wm,
	}
}

// ScanAndAssign 執行掃描與任務分派的核心業務邏輯
func (s *OrchestratorService) ScanAndAssign(ctx context.Context, allowedProjects, allowedAuthors []string) error {
	slog.Debug("Scanning GitLab Todos")
	todos, err := s.gitlabRepo.FetchPendingTodos(ctx)
	if err != nil {
		return fmt.Errorf("fetch todos failed: %w", err)
	}

	if len(todos) == 0 {
		slog.Debug("Scan complete: 0 pending Todos found")
		return nil
	}

	slog.Info("Pending Todos found", "count", len(todos))

	for _, todo := range todos {
		mr := todo.MergeRequest
		projectPath := todo.Project

		// 1. 狀態過濾
		if strings.ToLower(mr.State) != "opened" {
			slog.Info("Cleaning up non-opened MR Todo", "todo_id", todo.ID, "mr_iid", mr.IID, "project", projectPath, "state", mr.State)
			_ = s.gitlabRepo.MarkTodoAsDone(ctx, todo.ID)
			continue
		}

		// 2. 專案白名單過濾
		if !s.isAllowed(projectPath, allowedProjects) {
			slog.Debug("Skipping Todo: project not allowed", "todo_id", todo.ID, "project", projectPath)
			continue
		}

		// 3. 作者白名單過濾
		if !s.isAllowed(mr.Author, allowedAuthors) {
			slog.Debug("Skipping Todo: author not allowed", "todo_id", todo.ID, "mr_iid", mr.IID, "author", mr.Author)
			continue
		}

		// 4. 檢查 Worker 是否忙碌
		if s.workerManager != nil && s.isReviewerBusy() {
			slog.Info("Reviewer is busy, postponing MR", "mr_iid", mr.IID)
			continue
		}

		// 5. 尋找本地工作區
		localPath, err := s.workspaceRepo.FindLocalPath(ctx, projectPath)
		if err != nil {
			slog.Error("Error locating local workspace", "project", projectPath, "error", err)
			continue
		}

		// 6. 分派任務給 Worker
		if s.workerManager != nil {
			s.assignToReviewer(mr, localPath)
			// 成功指派後標記為已處理
			_ = s.gitlabRepo.MarkTodoAsDone(ctx, todo.ID)
		} else {
			slog.Info("Mock mode: task assignment", "mr_iid", mr.IID, "workspace", localPath)
		}
	}

	return nil
}

func (s *OrchestratorService) isAllowed(target string, allowedList []string) bool {
	if len(allowedList) == 0 {
		return true
	}
	target = strings.ToLower(strings.TrimSpace(target))
	for _, a := range allowedList {
		if strings.ToLower(strings.TrimSpace(a)) == target {
			return true
		}
	}
	return false
}

func (s *OrchestratorService) isReviewerBusy() bool {
	for _, w := range s.workerManager.Workers {
		if w.Config.ID == "reviewer" && w.IsBusy() {
			return true
		}
	}
	return false
}

func (s *OrchestratorService) assignToReviewer(mr MergeRequest, localPath string) {
	for _, w := range s.workerManager.Workers {
		if w.Config.ID == "reviewer" {
			if w.Config.Workspace != localPath {
				slog.Info("Switching reviewer workspace", "from", w.Config.Workspace, "to", localPath)
				w.Stop()
				w.Config.Workspace = localPath
				w.Start()
				time.Sleep(15 * time.Second)
			}
			instruction := fmt.Sprintf("請開始評審 Merge Request %d。網址為：%s\n", mr.IID, mr.WebURL)
			w.SendInput(instruction)
			slog.Info("Assigned task to reviewer", "mr_iid", mr.IID)
		}
	}
}
