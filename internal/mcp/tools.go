package mcp

import (
	"encoding/json"
	"fmt"
	"gemini-collaborator-go/internal/repository"
)

type ToolHandler struct {
	repo repository.TaskRepository
}

func NewToolHandler(repo repository.TaskRepository) *ToolHandler {
	return &ToolHandler{repo: repo}
}

func (h *ToolHandler) PollAvailableTasks(args map[string]interface{}) (interface{}, error) {
	tagsRaw, ok := args["tags"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'tags' argument")
	}

	tags := make([]string, len(tagsRaw))
	for i, v := range tagsRaw {
		tags[i] = fmt.Sprint(v)
	}

	tasks, err := h.repo.ListTasksByTags(tags, repository.StatusIdle)
	if err != nil {
		return nil, err
	}

	// 如果沒有 IDLE 任務，也檢查 REVISING 任務
	revisingTasks, err := h.repo.ListTasksByTags(tags, repository.StatusRevising)
	if err == nil {
		tasks = append(tasks, revisingTasks...)
	}

	return tasks, nil
}

func (h *ToolHandler) UpdateTaskStatus(args map[string]interface{}) (interface{}, error) {
	taskID, ok := args["taskId"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'taskId' argument")
	}

	nextStatusStr, ok := args["nextStatus"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'nextStatus' argument")
	}

	result, _ := args["result"].(string)

	err := h.repo.UpdateTaskStatus(taskID, repository.TaskStatus(nextStatusStr), result)
	if err != nil {
		return nil, err
	}

	return map[string]string{"status": "ok"}, nil
}

func (h *ToolHandler) ReadTaskContext(args map[string]interface{}) (interface{}, error) {
	taskID, ok := args["taskId"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'taskId' argument")
	}

	task, err := h.repo.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found: %s", taskID)
	}

	return task, nil
}

func (h *ToolHandler) ListTools() []Tool {
	return []Tool{
		{
			Name:        "poll_available_tasks",
			Description: "根據具備的標籤領取待處理的任務。",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"tags": map[string]interface{}{
						"type":        "array",
						"items":       map[string]string{"type": "string"},
						"description": "當前 Worker 具備的標籤",
					},
				},
				Required: []string{"tags"},
			},
		},
		{
			Name:        "update_task_status",
			Description: "更新任務進度或回報執行結果。",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"taskId": map[string]interface{}{
						"type": "string",
					},
					"nextStatus": map[string]interface{}{
						"type": "string",
						"enum": []string{"IN_PROGRESS", "AWAITING_CI", "READY_FOR_REVIEW", "FINISHED", "FAILED"},
					},
					"result": map[string]interface{}{
						"type": "string",
					},
				},
				Required: []string{"taskId", "nextStatus"},
			},
		},
		{
			Name:        "read_task_context",
			Description: "獲取任務的完整上下文 (Payload)。",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"taskId": map[string]interface{}{
						"type": "string",
					},
				},
				Required: []string{"taskId"},
			},
		},
	}
}

func (h *ToolHandler) HandleCallTool(req CallToolRequest) (interface{}, error) {
	switch req.Name {
	case "poll_available_tasks":
		return h.PollAvailableTasks(req.Arguments)
	case "update_task_status":
		return h.UpdateTaskStatus(req.Arguments)
	case "read_task_context":
		return h.ReadTaskContext(req.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", req.Name)
	}
}

func ToCallToolResult(result interface{}) CallToolResult {
	b, _ := json.MarshalIndent(result, "", "  ")
	return CallToolResult{
		Content: []TextContent{
			{
				Type: "text",
				Text: string(b),
			},
		},
	}
}
