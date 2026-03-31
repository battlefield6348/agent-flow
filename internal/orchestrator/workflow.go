package orchestrator

import (
	"context"
	"fmt"
	"log"
	"time"

	"gemini-collaborator-go/internal/gitlab"
	"gemini-collaborator-go/internal/repository"
)

type WorkflowEngine struct {
	repo    repository.TaskRepository
	gitlab  *gitlab.Adapter
	project string
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewWorkflowEngine(repo repository.TaskRepository, gitlab *gitlab.Adapter, projectID string) *WorkflowEngine {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkflowEngine{
		repo:    repo,
		gitlab:  gitlab,
		project: projectID,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start 啟動工作流引擎輪詢
func (e *WorkflowEngine) Start(pollInterval time.Duration) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	log.Printf("[Workflow] Engine started for project %s. Polling interval: %v", e.project, pollInterval)

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			if err := e.synchronizeWorkflow(); err != nil {
				log.Printf("[Workflow] synchronization error: %v", err)
			}
		}
	}
}

func (e *WorkflowEngine) synchronizeWorkflow() error {
	// 1. 查詢本地所有正在進行中的任務 (這裡可以用 repository.ListTasksByTags 如果有分頁或全查的功能)
	// 暫時以簡化版全查流程 (在 Repo 中我們定義了 ListTasksByTags，
	// 此處我們可以透過查詢 IDLE, IN_PROGRESS 等狀態來決定同步動作)

	// 2. 輪詢 GitLab 的 MR 狀態，此處示範邏輯：
	//    a. 發現新 MR 且無對應 Local 任務 -> 建立 Coder 任務
	//    b. Coder 已回報推送成功 -> 監控 CI Status
	//    c. CI Status = Success -> 指派 Reviewer 任務

	mrs, err := e.gitlab.ListProjectMRs(e.project, "opened", nil)
	if err != nil {
		return fmt.Errorf("failed to list project MRs: %w", err)
	}

	for _, mr := range mrs {
		// 檢查 CI 狀態來轉化工作流
		status, _ := e.gitlab.GetMRPipelineStatus(e.project, mr.IID)

		if status == "success" {
			// 如果 CI 過關且本地沒完成過 review，指派 Reviewer
			// 此處可以與 TaskRepository.ListTasksByTags 進一步整合判斷是否已存在
			log.Printf("[Workflow] MR %d CI Passed. Ready for Reviewer matching.", mr.IID)

			// 範例：建立 Review 任務
			// _ = e.repo.CreateTask(&repository.Task{
			// 	WorkflowID: fmt.Sprintf("mr-%d", mr.IID),
			// 	Status:     repository.StatusReadyForReview,
			// 	TargetTags: []string{"reviewer"},
			// })
		}
	}

	return nil
}

func (e *WorkflowEngine) Stop() {
	e.cancel()
}
