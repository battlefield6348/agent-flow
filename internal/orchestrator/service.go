package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	coderAgentID    = "coder"
	reviewerAgentID = "reviewer"
)

// GitLabRepository 定義與 GitLab 交互的介面 (Port)
type GitLabRepository interface {
	FetchPendingTodos(ctx context.Context) ([]Todo, error)
	MarkTodoAsDone(ctx context.Context, todoID int) error
	GetUsername(ctx context.Context) (string, error)
	FetchMergeRequestPipelines(ctx context.Context, projectPath string, mrIID int) ([]Pipeline, error)
	FetchMergeRequestNotes(ctx context.Context, projectPath string, mrIID int) ([]Note, error)
}

// WorkspaceRepository 定義本地工作區管理的介面 (Port)
type WorkspaceRepository interface {
	FindLocalPath(ctx context.Context, projectPath string) (string, error)
}

// OrchestratorService 負責協調任務排程的業務邏輯 (Use Case)
type OrchestratorService struct {
	gitlabRepo     GitLabRepository
	workspaceRepo  WorkspaceRepository
	workerManager  *WorkerManager
	checkCISuccess bool
	mu             sync.RWMutex
}

func NewOrchestratorService(gl GitLabRepository, ws WorkspaceRepository, wm *WorkerManager) *OrchestratorService {
	return &OrchestratorService{
		gitlabRepo:    gl,
		workspaceRepo: ws,
		workerManager: wm,
	}
}

func (s *OrchestratorService) SetCheckCISuccess(val bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkCISuccess = val
}

func (s *OrchestratorService) CheckCISuccess() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkCISuccess
}

// ScanAndAssignForAgent 針對特定的 Agent 執行掃描與任務分派的核心業務邏輯
func (s *OrchestratorService) ScanAndAssignForAgent(ctx context.Context, agentID string, repo GitLabRepository, allowedProjects, allowedAuthors []string) error {
	slog.Debug("Scanning GitLab Todos", "agent_id", agentID)
	todos, err := repo.FetchPendingTodos(ctx)
	if err != nil {
		return fmt.Errorf("fetch todos failed: %w", err)
	}

	if len(todos) == 0 {
		slog.Info("Scan complete: 0 pending Todos found", "agent_id", agentID)
		return nil
	}

	slog.Info("Pending Todos found", "agent_id", agentID, "count", len(todos))

	for _, todo := range todos {
		mr := todo.MergeRequest
		projectPath := todo.Project

		// 僅處理開啟狀態的 Merge Request，避免對已合併或關閉的任務進行無謂的操作
		if strings.ToLower(mr.State) != "opened" {
			slog.Info("Cleaning up non-opened MR Todo", "todo_id", todo.ID, "mr_iid", mr.IID, "project", projectPath, "state", mr.State)
			_ = repo.MarkTodoAsDone(ctx, todo.ID)
			continue
		}

		// 根據專案白名單過濾，確保僅在授權的專案範圍內運作
		if !s.isAllowed(projectPath, allowedProjects) {
			slog.Info("Skipping Todo: project not allowed", "todo_id", todo.ID, "project", projectPath)
			continue
		}

		// 根據作者白名單過濾，用於限定特定開發者的 MR 評審任務
		if !s.isAllowed(mr.Author, allowedAuthors) {
			slog.Info("Skipping Todo: author not allowed", "todo_id", todo.ID, "mr_iid", mr.IID, "author", mr.Author)
			continue
		}

		// 檢查 Worker 忙碌狀態以實現背壓控流，避免資源競爭或重複指派
		if s.workerManager != nil && s.isWorkerBusy(agentID) {
			slog.Info("Worker is busy, postponing MR", "worker_id", agentID, "mr_iid", mr.IID)
			continue
		}

		var notes []Note
		if agentID == coderAgentID {
			notes, err = repo.FetchMergeRequestNotes(ctx, projectPath, mr.IID)
			if err != nil {
				slog.Error("Failed to fetch notes for coder Todo", "mr_iid", mr.IID, "error", err)
				continue
			}
			if !hasRequestedChanges(notes) {
				if err := repo.MarkTodoAsDone(ctx, todo.ID); err != nil {
					slog.Error("Failed to mark coder Todo as done", "todo_id", todo.ID, "error", err)
				}
				continue
			}
		}

		// 檢查 CI 狀態
		if s.CheckCISuccess() {
			pipelines, err := repo.FetchMergeRequestPipelines(ctx, projectPath, mr.IID)
			if err != nil {
				slog.Error("Failed to fetch pipelines for MR", "project", projectPath, "mr_iid", mr.IID, "error", err)
				continue
			}

			if len(pipelines) > 0 {
				latestStatus := pipelines[0].Status
				if latestStatus != "success" {
					slog.Info("CI is not successful yet, skipping assignment", "mr_iid", mr.IID, "status", latestStatus)
					continue
				}
			} else {
				slog.Info("No associated CI/pipelines found, proceeding", "mr_iid", mr.IID)
			}
		}

		// 定位本地工作區路徑，以便 Worker 能在正確的環境中執行靜態分析或測試
		localPath, err := s.workspaceRepo.FindLocalPath(ctx, projectPath)
		if err != nil {
			slog.Error("Error locating local workspace", "project", projectPath, "error", err)
			continue
		}

		// 分派任務給底層 Worker 執行；若為 Mock 模式則僅記錄 Log
		if s.workerManager != nil {
			username, err := repo.GetUsername(ctx)
			if err != nil {
				slog.Error("Failed to get agent username", "agent_id", agentID, "error", err)
				continue
			}
			if notes == nil {
				notes, err = repo.FetchMergeRequestNotes(ctx, projectPath, mr.IID)
				if err != nil {
					slog.Error("Failed to fetch notes before assignment", "mr_iid", mr.IID, "error", err)
					continue
				}
			}
			lastNoteID := highestNoteIDByAuthor(notes, username)
			s.assignToWorker(agentID, mr, localPath, func(string) {
				updatedNotes, err := repo.FetchMergeRequestNotes(ctx, projectPath, mr.IID)
				if err != nil {
					slog.Error("Failed to fetch notes after worker success", "mr_iid", mr.IID, "error", err)
					return
				}
				for _, note := range updatedNotes {
					if note.ID > lastNoteID && note.Author == username && isValidCompletionNote(agentID, note.Body) {
						if err := repo.MarkTodoAsDone(ctx, todo.ID); err != nil {
							slog.Error("Failed to mark completed Todo as done", "todo_id", todo.ID, "error", err)
						}
						return
					}
				}
			})
		} else {
			slog.Info("Mock mode: task assignment", "agent_id", agentID, "mr_iid", mr.IID, "workspace", localPath)
		}
	}

	return nil
}

func hasRequestedChanges(notes []Note) bool {
	latestID := 0
	latestConclusion := ""
	for _, note := range notes {
		if note.ID > latestID && (strings.HasPrefix(note.Body, "## 審查結論") || strings.HasPrefix(note.Body, "### 結論")) {
			latestID = note.ID
			latestConclusion = note.Body
		}
	}
	return strings.Contains(latestConclusion, "需修改後再審")
}

func highestNoteIDByAuthor(notes []Note, username string) int {
	lastID := 0
	for _, note := range notes {
		if note.Author == username && note.ID > lastID {
			lastID = note.ID
		}
	}
	return lastID
}

func isValidCompletionNote(agentID, body string) bool {
	if agentID == coderAgentID {
		return strings.HasPrefix(body, "## 修正回覆")
	}
	return agentID == reviewerAgentID && (strings.HasPrefix(body, "## 審查結論") || strings.HasPrefix(body, "### 結論"))
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

func (s *OrchestratorService) isWorkerBusy(agentID string) bool {
	for _, w := range s.workerManager.Workers {
		if w.Config.ID == agentID && w.IsBusy() {
			return true
		}
	}
	return false
}

func (s *OrchestratorService) assignToWorker(agentID string, mr MergeRequest, localPath string, onSuccess func(string)) {
	for _, w := range s.workerManager.Workers {
		if w.Config.ID == agentID {
			if w.Config.Workspace != localPath {
				slog.Info("Switching worker workspace", "worker_id", agentID, "from", w.Config.Workspace, "to", localPath)
				w.Stop()
				w.Config.Workspace = localPath
				w.Start()
				time.Sleep(15 * time.Second)
			}
			var actionName string
			if agentID == reviewerAgentID {
				actionName = "評審"
			} else {
				actionName = "處理"
			}
			instruction := fmt.Sprintf("請開始%s Merge Request %d。網址為：%s", actionName, mr.IID, mr.WebURL)
			if agentID == coderAgentID {
				instruction = fmt.Sprintf("請閱讀最新的審查結論，於同一個 Merge Request 分支完成修正，並發表以「## 修正回覆」開頭的留言。Merge Request %d。網址為：%s", mr.IID, mr.WebURL)
			}
			if w.Config.PromptSuffix != "" {
				instruction += w.Config.PromptSuffix
			}
			instruction += "\n"
			w.SendTask(WorkerTask{Text: instruction, OnSuccess: onSuccess})
			slog.Info("Assigned task to worker", "worker_id", agentID, "mr_iid", mr.IID)
		}
	}
}
