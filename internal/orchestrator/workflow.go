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
	mrs, err := e.gitlab.ListProjectMRs(e.project, "opened", nil)
	if err != nil {
		return fmt.Errorf("failed to list project MRs: %w", err)
	}

	for _, mr := range mrs {
		workflowID := fmt.Sprintf("mr-%d", mr.IID)
		task, err := e.repo.GetTaskByWorkflowID(workflowID)
		if err != nil {
			return err
		}

		// 1. 如果是全新的 MR (且為 Draft 或有標籤)，分配給 Coder
		if task == nil {
			if mr.Draft || e.hasLabel(mr.Labels, "bot-request") {
				log.Printf("[Workflow] New MR detected (%d). Creating Coder task.", mr.IID)
				err := e.repo.CreateTask(&repository.Task{
					WorkflowID: workflowID,
					Status:     repository.StatusIdle,
					TargetTags: []string{"coder", "golang", "dev"},
					Payload:    fmt.Sprintf("MR_IID:%d, Branch:%s", mr.IID, mr.SourceBranch),
				})
				if err != nil {
					log.Printf("[Workflow] failed to create task: %v", err)
				}
			}
			continue
		}

		// 2. 如果任務處理中，監控 CI 狀態進行「接棒」
		e.handleTaskTransition(task, mr.IID)
	}

	return nil
}

func (e *WorkflowEngine) handleTaskTransition(task *repository.Task, mrIID int) {
	// 如果任務處於「等待 CI」或「正在進行」，我們檢查最新 Pipeline
	if task.Status == repository.StatusInProgress || task.Status == repository.StatusAwaitingCI {
		status, _ := e.gitlab.GetMRPipelineStatus(e.project, mrIID)

		if status == "success" {
			log.Printf("[Workflow] MR %d CI Passed. Transitioning to Reviewer.", mrIID)

			// 更新標籤為 reviewer，狀態設為 IDLE 重啟領取流程
			task.Status = repository.StatusIdle
			task.TargetTags = []string{"reviewer", "security"}
			err := e.repo.UpdateTask(task)
			if err != nil {
				log.Printf("[Workflow] failed to update task target tags: %v", err)
			}
		}
	}
}

func (e *WorkflowEngine) hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

func (e *WorkflowEngine) Stop() {
	e.cancel()
}
