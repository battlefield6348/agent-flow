package orchestrator

import (
	"context"
	"testing"
)

type MockGitLabRepository struct {
	Todos     []Todo
	Pipelines []Pipeline
	Err       error
}

func (m *MockGitLabRepository) FetchPendingTodos(ctx context.Context) ([]Todo, error) {
	return m.Todos, m.Err
}
func (m *MockGitLabRepository) MarkTodoAsDone(ctx context.Context, todoID int) error {
	return nil
}
func (m *MockGitLabRepository) GetUsername(ctx context.Context) (string, error) {
	return "mockuser", nil
}
func (m *MockGitLabRepository) FetchMergeRequestPipelines(ctx context.Context, projectPath string, mrIID int) ([]Pipeline, error) {
	return m.Pipelines, m.Err
}

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
					IID:    101,
					State:  "opened",
					WebURL: "http://gitlab.com/mr/101",
					Author: "author1",
				},
			},
		},
	}
	ws := &MockWorkspaceRepository{Path: "/local/path"}

	service := NewOrchestratorService(gl, ws, nil)

	err := service.ScanAndAssign(context.Background(), []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssign failed: %v", err)
	}
}

func TestOrchestratorService_ScanAndAssign_CIChecks(t *testing.T) {
	todo := Todo{
		ID:      1,
		Project: "group/project",
		MergeRequest: MergeRequest{
			IID:    101,
			State:  "opened",
			WebURL: "http://gitlab.com/mr/101",
			Author: "author1",
		},
	}

	t.Run("CI check is disabled", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo},
			Pipelines: []Pipeline{
				{ID: 1, Status: "failed"},
			},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		service := NewOrchestratorService(gl, ws, nil)
		service.SetCheckCISuccess(false)

		err := service.ScanAndAssign(context.Background(), []string{"group/project"}, []string{"author1"})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
	})

	t.Run("CI check is enabled and status is success", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo},
			Pipelines: []Pipeline{
				{ID: 1, Status: "success"},
			},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		service := NewOrchestratorService(gl, ws, nil)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssign(context.Background(), []string{"group/project"}, []string{"author1"})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
	})

	t.Run("CI check is enabled and status is running", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo},
			Pipelines: []Pipeline{
				{ID: 1, Status: "running"},
			},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		service := NewOrchestratorService(gl, ws, nil)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssign(context.Background(), []string{"group/project"}, []string{"author1"})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
	})

	t.Run("CI check is enabled and no pipelines exist", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos:     []Todo{todo},
			Pipelines: []Pipeline{},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		service := NewOrchestratorService(gl, ws, nil)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssign(context.Background(), []string{"group/project"}, []string{"author1"})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
	})
}

