package orchestrator

import (
	"context"
	"testing"
)

type MockGitLabRepository struct {
	Todos          []Todo
	Pipelines      []Pipeline
	Notes          []Note
	NotesByCall    [][]Note
	NoteErrors     []error
	MarkedTodoIDs  []int
	Username       string
	Err            error
	NoteFetchCount int
}

func (m *MockGitLabRepository) FetchPendingTodos(ctx context.Context) ([]Todo, error) {
	return m.Todos, m.Err
}
func (m *MockGitLabRepository) MarkTodoAsDone(ctx context.Context, todoID int) error {
	m.MarkedTodoIDs = append(m.MarkedTodoIDs, todoID)
	return nil
}
func (m *MockGitLabRepository) GetUsername(ctx context.Context) (string, error) {
	if m.Username != "" {
		return m.Username, m.Err
	}
	return "mockuser", nil
}
func (m *MockGitLabRepository) FetchMergeRequestPipelines(ctx context.Context, projectPath string, mrIID int) ([]Pipeline, error) {
	return m.Pipelines, m.Err
}
func (m *MockGitLabRepository) FetchMergeRequestNotes(ctx context.Context, projectPath string, mrIID int) ([]Note, error) {
	index := m.NoteFetchCount
	m.NoteFetchCount++
	err := m.Err
	if index < len(m.NoteErrors) {
		err = m.NoteErrors[index]
	}
	if index < len(m.NotesByCall) {
		return m.NotesByCall[index], err
	}
	return m.Notes, err
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
	dispatcher := &MockTaskDispatcher{}

	service := NewOrchestratorService(gl, ws, dispatcher)

	err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssignForAgent failed: %v", err)
	}
	if len(dispatcher.DispatchedTasks) != 1 {
		t.Fatalf("Expected 1 dispatched task, got %d", len(dispatcher.DispatchedTasks))
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
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)
		service.SetCheckCISuccess(false)

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("Expected 1 dispatched task, got %d", len(dispatcher.DispatchedTasks))
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
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("Expected 1 dispatched task, got %d", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("CI check is enabled and status is failed", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo},
			Pipelines: []Pipeline{
				{ID: 1, Status: "failed"},
			},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("Expected 0 dispatched tasks due to failed CI, got %d", len(dispatcher.DispatchedTasks))
		}
	})
}

func TestOrchestratorService_CoderTodoLifecycle(t *testing.T) {
	todo := Todo{
		ID:           1,
		Project:      "group/project",
		MergeRequest: MergeRequest{IID: 101, State: "opened", WebURL: "http://gitlab.com/mr/101", Author: "author1"},
	}

	t.Run("coder skips non-fix todo", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}, Notes: []Note{{ID: 1, Body: "## 審查結論\n可以合併"}}}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, dispatcher)

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(gl.MarkedTodoIDs) != 1 || gl.MarkedTodoIDs[0] != todo.ID {
			t.Fatalf("Todo completion = %v, want [%d]", gl.MarkedTodoIDs, todo.ID)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("coder received non-fix todo, count=%d", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("coder dispatches requested-fix todo", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}, Username: "bot", Notes: []Note{{ID: 1, Body: "## 審查結論\n需修改後再審"}}}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, dispatcher)

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("coder expected 1 task, got %d", len(dispatcher.DispatchedTasks))
		}
	})
}

func TestOrchestratorService_WithTaskDispatcher(t *testing.T) {
	todo := Todo{ID: 10, Project: "group/proj", MergeRequest: MergeRequest{IID: 200, State: "opened", WebURL: "http://gitlab.com/mr/200", Author: "author1"}}
	gl := &MockGitLabRepository{Todos: []Todo{todo}}
	ws := &MockWorkspaceRepository{Path: "/workspace/proj"}

	t.Run("busy dispatcher postpones task", func(t *testing.T) {
		dispatcher := &MockTaskDispatcher{BusyMap: map[string]bool{"reviewer": true}}
		service := NewOrchestratorServiceWithDispatcher(gl, ws, dispatcher)
		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("busy 時不應指派任務，但指派了 %d 個", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("idle dispatcher assigns task", func(t *testing.T) {
		dispatcher := &MockTaskDispatcher{BusyMap: map[string]bool{"reviewer": false}}
		service := NewOrchestratorServiceWithDispatcher(gl, ws, dispatcher)
		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("期望指派 1 個任務，但得到了 %d 個", len(dispatcher.DispatchedTasks))
		}
		task := dispatcher.DispatchedTasks[0]
		if task.AgentID != "reviewer" || task.MRIID != 200 || task.Workspace != "/workspace/proj" {
			t.Errorf("派發任務內容不符合期望: %+v", task)
		}
	})
}
