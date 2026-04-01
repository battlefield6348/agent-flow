package orchestrator

import (
	"encoding/json"

	"gemini-collaborator-go/internal/repository"

	"github.com/google/uuid"
)

type TaskHandler struct {
	repo repository.TaskRepository
}

func NewTaskHandler(repo repository.TaskRepository) *TaskHandler {
	return &TaskHandler{repo: repo}
}

// 這裡保留原本的業務邏輯，未來如果需要實作 Stdio MCP 或由 Telegram 觸發任務時可以使用
func (h *TaskHandler) CreateInstruction(goal, project string) (string, string, error) {
	workflowID := uuid.New().String()
	payload, _ := json.Marshal(map[string]interface{}{
		"goal":    goal,
		"project": project,
	})

	newTask := &repository.Task{
		WorkflowID: workflowID,
		Status:     repository.StatusIdle,
		TargetTags: []string{"coder"},
		Payload:    string(payload),
	}

	if err := h.repo.CreateTask(newTask); err != nil {
		return "", "", err
	}

	return workflowID, newTask.ID, nil
}
