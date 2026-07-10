package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// collaborator 角色 id。必須與 configs/config.yaml 內 collaborators[].id 完全一致，
// 否則指令產生、完成判定、final-response 解析都會靜默退回泛用行為。
const (
	agentIDReviewer = "reviewer"
	agentIDCoder    = "coder"
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
}

func NewOrchestratorService(gl GitLabRepository, ws WorkspaceRepository, wm *WorkerManager) *OrchestratorService {
	return &OrchestratorService{
		gitlabRepo:    gl,
		workspaceRepo: ws,
		workerManager: wm,
	}
}

func (s *OrchestratorService) SetCheckCISuccess(val bool) {
	s.checkCISuccess = val
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

	expectedGitLabUsername := ""
	if agentID == agentIDCoder {
		expectedGitLabUsername, err = repo.GetUsername(ctx)
		if err != nil {
			return fmt.Errorf("get username for agent %q failed: %w", agentID, err)
		}
	}

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

		// coder 守門：coder 身分＝使用者本人，任何在白名單專案內 @mention/assign 到本帳號的 todo
		// 都會落到這裡。只有當 MR 上已存在 reviewer 的「## 審查結論」留言時，才代表這是「請 coder 修正
		// 審查意見」的 todo，值得指派。否則（他人 @ 提及、尚無審查等）直接把 todo 標 done，
		// 避免 coder 產不出「## 修正回覆」而每輪無限重跑，也避免在使用者本人帳號上做非預期的自動修改。
		if agentID == agentIDCoder {
			needsFix, err := s.mrNeedsCoderFix(ctx, repo, projectPath, mr.IID)
			if err != nil {
				slog.Error("Failed to inspect MR notes for coder gate", "project", projectPath, "mr_iid", mr.IID, "error", err)
				continue
			}
			if !needsFix {
				slog.Info("Coder todo has no reviewer request for changes; marking done to avoid rerun", "todo_id", todo.ID, "mr_iid", mr.IID, "project", projectPath)
				_ = repo.MarkTodoAsDone(ctx, todo.ID)
				continue
			}
		}

		if agentID == agentIDReviewer {
			alreadyHandled, err := s.reviewerTodoAlreadyHandled(ctx, repo, projectPath, mr.IID)
			if err != nil {
				slog.Error("Failed to inspect MR notes for reviewer dedupe", "project", projectPath, "mr_iid", mr.IID, "error", err)
				continue
			}
			if alreadyHandled {
				slog.Info("Reviewer todo already covered by latest review on current diff; marking done", "todo_id", todo.ID, "mr_iid", mr.IID, "project", projectPath)
				_ = repo.MarkTodoAsDone(ctx, todo.ID)
				continue
			}
		}

		// 檢查 CI 狀態
		if s.checkCISuccess {
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
			latestBotNoteID, botUsername, err := s.getLatestBotNoteState(ctx, repo, projectPath, mr.IID)
			if err != nil {
				slog.Error("Failed to inspect current MR notes before assignment", "project", projectPath, "mr_iid", mr.IID, "error", err)
				continue
			}

			s.assignToWorker(agentID, mr, localPath, expectedGitLabUsername, func(_ string) {
				if err := s.handleWorkerSuccess(ctx, repo, agentID, todo.ID, projectPath, mr, botUsername, latestBotNoteID); err != nil {
					slog.Error("Failed to finalize worker result", "todo_id", todo.ID, "mr_iid", mr.IID, "error", err)
				}
			})
		} else {
			slog.Info("Mock mode: task assignment", "agent_id", agentID, "mr_iid", mr.IID, "workspace", localPath)
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

func (s *OrchestratorService) isWorkerBusy(agentID string) bool {
	for _, w := range s.workerManager.Workers {
		if w.Config.ID == agentID && w.IsBusy() {
			return true
		}
	}
	return false
}

func (s *OrchestratorService) assignToWorker(agentID string, mr MergeRequest, localPath string, expectedGitLabUsername string, onSuccess func(string)) {
	for _, w := range s.workerManager.Workers {
		if w.Config.ID == agentID {
			if w.Config.Workspace != localPath {
				slog.Info("Switching worker workspace", "worker_id", agentID, "from", w.Config.Workspace, "to", localPath)
				w.Stop()
				w.Config.Workspace = localPath
				w.Start()
				time.Sleep(15 * time.Second)
			}
			instruction := buildWorkerInstruction(agentID, mr)
			w.SendTask(WorkerTask{
				Text:                   instruction,
				ExpectedGitLabUsername: expectedGitLabUsername,
				OnSuccess:              onSuccess,
			})
			slog.Info("Assigned task to worker", "worker_id", agentID, "mr_iid", mr.IID)
		}
	}
}

func buildWorkerInstruction(agentID string, mr MergeRequest) string {
	switch agentID {
	case agentIDReviewer:
		return fmt.Sprintf("請開始評審 Merge Request %d。網址為：%s\n請直接在 GitLab 的 Merge Request 留言張貼最終審查內容；不要貼思考過程、操作 transcript、狀態列或工具輸出。留言完成後，再把相同的最終審查內容輸出到終端。\n", mr.IID, mr.WebURL)
	case agentIDCoder:
		return fmt.Sprintf("請處理 Merge Request %d 上的最新審查意見。網址為：%s\n請讀取該 MR 上最新一則以「## 審查結論」開頭的審查留言，逐項修正其中的必修問題，並將修正 push 到該 MR 的同一個來源分支（不要另開新分支或新 MR）。完成後，在該 MR 留一則以「## 修正回覆」開頭的留言，逐項說明每條必修問題如何處理；不要貼思考過程、操作 transcript、狀態列或工具輸出。留言完成後，再把相同的最終內容輸出到終端。\n", mr.IID, mr.WebURL)
	}
	return fmt.Sprintf("請開始處理 Merge Request %d。網址為：%s\n", mr.IID, mr.WebURL)
}

func (s *OrchestratorService) getLatestBotNoteState(ctx context.Context, repo GitLabRepository, projectPath string, mrIID int) (int, string, error) {
	botUsername, err := repo.GetUsername(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("get bot username failed: %w", err)
	}
	notes, err := repo.FetchMergeRequestNotes(ctx, projectPath, mrIID)
	if err != nil {
		return 0, "", fmt.Errorf("fetch MR notes failed: %w", err)
	}

	latestID := 0
	for _, note := range notes {
		if note.Author == botUsername && note.ID > latestID {
			latestID = note.ID
		}
	}
	return latestID, botUsername, nil
}

func (s *OrchestratorService) handleWorkerSuccess(ctx context.Context, repo GitLabRepository, agentID string, todoID int, projectPath string, mr MergeRequest, botUsername string, previousNoteID int) error {
	notes, err := repo.FetchMergeRequestNotes(ctx, projectPath, mr.IID)
	if err != nil {
		return fmt.Errorf("fetch MR notes failed: %w", err)
	}

	latestID := previousNoteID
	latestBody := ""
	for _, note := range notes {
		if note.Author == botUsername && note.ID > latestID {
			latestID = note.ID
			latestBody = strings.TrimSpace(note.Body)
		}
	}
	if latestID == previousNoteID {
		return fmt.Errorf("no new bot MR note detected")
	}
	if !isValidCompletionNote(agentID, latestBody) {
		return fmt.Errorf("latest bot MR note is not a valid completion note for agent %q", agentID)
	}
	if err := repo.MarkTodoAsDone(ctx, todoID); err != nil {
		return fmt.Errorf("mark todo done failed: %w", err)
	}
	slog.Info("Detected bot MR note and marked todo as done", "todo_id", todoID, "mr_iid", mr.IID, "project", projectPath, "note_id", latestID)
	return nil
}

// mrNeedsCoderFix 判斷 MR 上是否有 reviewer 明確「要求修正」的審查結論，作為 coder 派工的守門條件。
// 僅「存在審查結論」還不夠——若結論是核准或僅待作者說明（無必修問題），指派 coder 會因無事可修而
// 產不出「## 修正回覆」、導致 todo 每輪重跑。因此收緊為：必須有以「## 審查結論」（或舊版「### 結論」）
// 開頭、且結論明確標示「需修改後再審」的留言，才放行；否則直接標 done，避免誤觸發與無限重跑。
func (s *OrchestratorService) mrNeedsCoderFix(ctx context.Context, repo GitLabRepository, projectPath string, mrIID int) (bool, error) {
	notes, err := repo.FetchMergeRequestNotes(ctx, projectPath, mrIID)
	if err != nil {
		return false, fmt.Errorf("fetch MR notes failed: %w", err)
	}
	for _, note := range notes {
		body := strings.TrimSpace(note.Body)
		if strings.HasPrefix(body, "## 審查結論") || strings.HasPrefix(body, "### 結論") {
			if strings.Contains(body, "需修改後再審") {
				return true, nil
			}
		}
	}
	return false, nil
}

// reviewerTodoAlreadyHandled 判斷 reviewer pending todo 是否只是重複 @mention。
// 規則：若目前 MR 上已存在一則有效 reviewer 審查結論，且在那之後沒有新的 commit system note，
// 就視為「當前 diff 已被審過」，新的 reviewer todo 直接標 done，避免同一個 SHA 重複審查。
func (s *OrchestratorService) reviewerTodoAlreadyHandled(ctx context.Context, repo GitLabRepository, projectPath string, mrIID int) (bool, error) {
	botUsername, err := repo.GetUsername(ctx)
	if err != nil {
		return false, fmt.Errorf("get bot username failed: %w", err)
	}

	notes, err := repo.FetchMergeRequestNotes(ctx, projectPath, mrIID)
	if err != nil {
		return false, fmt.Errorf("fetch MR notes failed: %w", err)
	}

	var latestReviewAt time.Time
	var latestCommitAt time.Time
	for _, note := range notes {
		body := strings.TrimSpace(note.Body)
		if note.Author == botUsername && isValidCompletionNote(agentIDReviewer, body) && note.CreatedAt.After(latestReviewAt) {
			latestReviewAt = note.CreatedAt
		}
		if note.System && isCommitSystemNote(body) && note.CreatedAt.After(latestCommitAt) {
			latestCommitAt = note.CreatedAt
		}
	}

	if latestReviewAt.IsZero() {
		return false, nil
	}

	return !latestCommitAt.After(latestReviewAt), nil
}

func isCommitSystemNote(body string) bool {
	body = strings.TrimSpace(body)
	return strings.HasPrefix(body, "added ") && strings.Contains(body, " commit")
}

// isValidCompletionNote 依角色判斷該 collaborator 貼出的最新留言是否為「有效的最終產出」，
// 只有有效時才會把對應 todo 標記為 done。不同角色的留言格式不同：
//   - reviewer：以「## 審查結論」（或舊版「### 結論」）開頭的結構化審查
//   - coder：以「## 修正回覆」開頭、逐項回應審查意見的修正說明
//
// 這可避免 CLI 把 TUI 畫面、狀態列或 transcript 當成留言時被誤判為完成。
func isValidCompletionNote(agentID, body string) bool {
	body = strings.TrimSpace(body)
	switch agentID {
	case agentIDCoder:
		return strings.HasPrefix(body, "## 修正回覆")
	default:
		return strings.HasPrefix(body, "## 審查結論") || strings.HasPrefix(body, "### 結論")
	}
}
