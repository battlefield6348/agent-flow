package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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

// OrchestratorService 負責協調任務排程的核心業務邏輯
type OrchestratorService struct {
	gitlabRepo     GitLabRepository
	workspaceRepo  WorkspaceRepository
	dispatcher     TaskDispatcher
	checkCISuccess bool
	mu             sync.RWMutex
}

func NewOrchestratorService(gl GitLabRepository, ws WorkspaceRepository, dispatcher TaskDispatcher) *OrchestratorService {
	return &OrchestratorService{
		gitlabRepo:    gl,
		workspaceRepo: ws,
		dispatcher:    dispatcher,
	}
}

func NewOrchestratorServiceWithDispatcher(gl GitLabRepository, ws WorkspaceRepository, dispatcher TaskDispatcher) *OrchestratorService {
	return &OrchestratorService{
		gitlabRepo:    gl,
		workspaceRepo: ws,
		dispatcher:    dispatcher,
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

		if strings.ToLower(mr.State) != "opened" {
			slog.Info("Cleaning up non-opened MR Todo", "todo_id", todo.ID, "mr_iid", mr.IID, "project", projectPath, "state", mr.State)
			_ = repo.MarkTodoAsDone(ctx, todo.ID)
			continue
		}

		if !s.isAllowed(projectPath, allowedProjects) {
			slog.Info("Skipping Todo: project not allowed", "todo_id", todo.ID, "project", projectPath)
			continue
		}

		if !s.isAllowed(mr.Author, allowedAuthors) {
			slog.Info("Skipping Todo: author not allowed", "todo_id", todo.ID, "mr_iid", mr.IID, "author", mr.Author)
			continue
		}

		if s.dispatcher != nil {
			busy, err := s.dispatcher.IsBusy(ctx, agentID)
			if err == nil && busy {
				slog.Info("Agent dispatcher is busy, postponing MR", "agent_id", agentID, "mr_iid", mr.IID)
				continue
			}
		}

		var notes []Note
		if agentID == coderAgentID {
			var err error
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

		localPath, err := s.workspaceRepo.FindLocalPath(ctx, projectPath)
		if err != nil {
			slog.Error("Error locating local workspace", "project", projectPath, "error", err)
			continue
		}

		if s.dispatcher != nil {
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

			err := s.dispatcher.DispatchTask(ctx, DispatchTaskInput{
				AgentID:     agentID,
				Workspace:   localPath,
				Instruction: instruction,
				MRIID:       mr.IID,
				MRWebURL:    mr.WebURL,
			})
			if err != nil {
				slog.Error("Failed to dispatch task via TaskDispatcher", "agent_id", agentID, "mr_iid", mr.IID, "error", err)
			} else {
				slog.Info("Successfully dispatched task via TaskDispatcher", "agent_id", agentID, "mr_iid", mr.IID)
			}
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
