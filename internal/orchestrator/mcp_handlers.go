package orchestrator

import (
	"encoding/json"
	"fmt"
	"gemini-collaborator-go/internal/mcp"
	"gemini-collaborator-go/internal/repository"
)

type MCPHandler struct {
	repo repository.TaskRepository
}

func RegisterMCPTools(server *mcp.Server, repo repository.TaskRepository) {
	h := &MCPHandler{repo: repo}

	// 1. 列出可領取的任務
	server.AddTool("list_tasks", "List tasks matching tags", h.handleListTasks)
	// 2. 領取任務
	server.AddTool("claim_task", "Claim a task to work on", h.handleClaimTask)
	// 3. 完成任務
	server.AddTool("finish_task", "Finish a task and report result", h.handleFinishTask)
}

func (h *MCPHandler) handleListTasks(params json.RawMessage) (interface{}, error) {
	type ListParams struct {
		Tags []string `json:"tags"`
	}
	var p ListParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	tasks, err := h.repo.ListTasksByTags(p.Tags, repository.StatusIdle)
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

func (h *MCPHandler) handleClaimTask(params json.RawMessage) (interface{}, error) {
	type ClaimParams struct {
		TaskID string `json:"task_id"`
	}
	var p ClaimParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	// 這裡的邏輯可以再強化，例如檢查任務是否真的處於 IDLE
	err := h.repo.UpdateTaskStatus(p.TaskID, repository.StatusInProgress, "")
	if err != nil {
		return nil, err
	}

	return map[string]string{"status": "success", "message": "task claimed"}, nil
}

func (h *MCPHandler) handleFinishTask(params json.RawMessage) (interface{}, error) {
	type FinishParams struct {
		TaskID string                `json:"task_id"`
		Result string                `json:"result"`
		Status repository.TaskStatus `json:"status"` // 如 REVIEW_PASSED, FINISHED 等
	}
	var p FinishParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}

	err := h.repo.UpdateTaskStatus(p.TaskID, p.Status, p.Result)
	if err != nil {
		return nil, err
	}

	return map[string]string{"status": "success", "message": fmt.Sprintf("task updated to %s", p.Status)}, nil
}
