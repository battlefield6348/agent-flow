package orchestrator

import (
	"context"
	"testing"
)

type MockGitLabRepository struct {
	Todos     []Todo
	Pipelines []Pipeline
	Notes     []Note
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
func (m *MockGitLabRepository) FetchMergeRequestNotes(ctx context.Context, projectPath string, mrIID int) ([]Note, error) {
	return m.Notes, m.Err
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

	err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssignForAgent failed: %v", err)
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

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
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

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
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

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
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

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
	})
}

func TestOrchestratorService_AssignToWorkerWithPromptSuffix(t *testing.T) {
	w := &Worker{
		Config: CollaboratorConfig{
			ID:           "coder",
			Cmd:          "codex",
			PromptSuffix: "，請立刻處理",
			Workspace:    "/local/path",
		},
		inputCh: make(chan string, 10),
	}

	wm := &WorkerManager{
		Workers: []*Worker{w},
	}

	service := NewOrchestratorService(nil, nil, wm)
	mr := MergeRequest{
		IID:    101,
		WebURL: "http://gitlab.com/mr/101",
	}

	service.assignToWorker("coder", mr, "/local/path")

	select {
	case sent := <-w.inputCh:
		expected := "請開始處理 Merge Request 101。網址為：http://gitlab.com/mr/101，請立刻處理\n"
		if sent != expected {
			t.Errorf("預期發送為 '%s'，但得到 '%s'", expected, sent)
		}
	default:
		t.Fatalf("預期有發送指令到 inputCh，但沒收到")
	}
}
