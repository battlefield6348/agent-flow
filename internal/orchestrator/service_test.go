package orchestrator

import (
	"context"
	"testing"
)

// MockGitLabRepository 實作 GitLabRepository 介面用於測試
type MockGitLabRepository struct {
	Todos []Todo
}

func (m *MockGitLabRepository) FetchPendingTodos(ctx context.Context) ([]Todo, error) {
	return m.Todos, nil
}
func (m *MockGitLabRepository) MarkTodoAsDone(ctx context.Context, todoID int) error {
	return nil
}
func (m *MockGitLabRepository) GetUsername(ctx context.Context) (string, error) {
	return "mockuser", nil
}

// MockWorkspaceRepository 實作 WorkspaceRepository 介面用於測試
type MockWorkspaceRepository struct {
	Path string
}

func (m *MockWorkspaceRepository) FindLocalPath(ctx context.Context, projectPath string) (string, error) {
	return m.Path, nil
}

func TestOrchestratorService_ScanAndAssign(t *testing.T) {
	gl := &MockGitLabRepository{
		Todos: []Todo{
			{
				ID:      1,
				Project: "group/project",
				MergeRequest: MergeRequest{
					IID:   101,
					State: "opened",
					WebURL: "http://gitlab.com/mr/101",
					Author: "author1",
				},
			},
		},
	}
	ws := &MockWorkspaceRepository{Path: "/local/path"}
	// 這裡需要一個 Mock WorkerManager，但因為 WorkerManager 目前較為複雜，
	// 我們先實作一個簡單的 OrchestratorService 並測試其邏輯流。
	
	service := NewOrchestratorService(gl, ws, nil)
	
	// 這裡可以測試 ScanAndAssign 的過濾邏輯等
	// 由於目前 ScanAndAssign 還沒實作，這是一個紅燈測試。
	err := service.ScanAndAssign(context.Background(), []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssign failed: %v", err)
	}
}
