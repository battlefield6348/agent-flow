package orchestrator

import (
	"context"
	"fmt"
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
	fmt.Println("[Orchestrator] Scanning GitLab Todos...")
	todos, err := s.gitlabRepo.FetchPendingTodos(ctx)
	if err != nil {
		return fmt.Errorf("fetch todos failed: %w", err)
	}

	if len(todos) == 0 {
		fmt.Println("[Orchestrator] Scan complete: 0 pending Todos found.")
		return nil
	}

	fmt.Printf("[Orchestrator] Found %d pending Todos.\n", len(todos))

	for _, todo := range todos {
		mr := todo.MergeRequest
		projectPath := todo.Project

		// 1. 狀態過濾
		if strings.ToLower(mr.State) != "opened" {
			fmt.Printf("[Orchestrator] Todo %d MR %d [%s] is not opened (%s), cleaning up...\n", todo.ID, mr.IID, projectPath, mr.State)
			_ = s.gitlabRepo.MarkTodoAsDone(ctx, todo.ID)
			continue
		}

		// 2. 專案白名單過濾
		if !s.isAllowed(projectPath, allowedProjects) {
			fmt.Printf("[Orchestrator] Todo %d [%s]: Skip (not in allowed projects)\n", todo.ID, projectPath)
			continue
		}

		// 3. 作者白名單過濾
		if !s.isAllowed(mr.Author, allowedAuthors) {
			fmt.Printf("[Orchestrator] Todo %d MR %d author '%s': Skip (not in allowed authors)\n", todo.ID, mr.IID, mr.Author)
			continue
		}

		// 4. 檢查 Worker 是否忙碌
		if s.workerManager != nil && s.isReviewerBusy() {
			fmt.Printf("[Orchestrator] Reviewer is busy, postponing MR %d...\n", mr.IID)
			continue
		}

		// 5. 尋找本地工作區
		localPath, err := s.workspaceRepo.FindLocalPath(ctx, projectPath)
		if err != nil {
			fmt.Printf("[Orchestrator] Error locating local workspace for %s: %v\n", projectPath, err)
			continue
		}

		// 6. 分派任務給 Worker
		if s.workerManager != nil {
			s.assignToReviewer(mr, localPath)
			// 成功指派後標記為已處理
			_ = s.gitlabRepo.MarkTodoAsDone(ctx, todo.ID)
		} else {
			fmt.Printf("[Orchestrator] Mock mode: Would assign MR %d to workspace %s\n", mr.IID, localPath)
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
				fmt.Printf("[Orchestrator] Switching reviewer workspace to %s\n", localPath)
				w.Stop()
				w.Config.Workspace = localPath
				w.Start()
				time.Sleep(15 * time.Second)
			}
			instruction := fmt.Sprintf("請開始評審 Merge Request %d。網址為：%s\n", mr.IID, mr.WebURL)
			w.SendInput(instruction)
		}
	}
}
