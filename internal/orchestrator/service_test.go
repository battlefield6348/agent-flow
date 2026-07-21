package orchestrator

import (
	"context"
	"errors"
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

func TestOrchestratorService_ReviewerCompletionLifecycle(t *testing.T) {
	todo := Todo{ID: 2, Project: "group/project", MergeRequest: MergeRequest{IID: 102, State: "opened", WebURL: "http://gitlab.com/mr/102", Author: "author1"}}
	gl := &MockGitLabRepository{Todos: []Todo{todo}, Username: "review-bot", NotesByCall: [][]Note{
		{{ID: 4, Author: "review-bot", Body: "舊留言"}},
		{{ID: 4, Author: "review-bot", Body: "舊留言"}, {ID: 5, Author: "review-bot", Body: "### 結論\n請修正"}},
	}}
	worker := &Worker{Config: CollaboratorConfig{ID: "reviewer", Workspace: "/local/path"}, inputCh: make(chan WorkerTask, 1)}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, &WorkerManager{Workers: []*Worker{worker}})

	if err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(gl.MarkedTodoIDs) != 0 {
		t.Fatalf("Todo completed before reviewer success: %v", gl.MarkedTodoIDs)
	}
	(<-worker.inputCh).OnSuccess("完成")
	if len(gl.MarkedTodoIDs) != 1 || gl.MarkedTodoIDs[0] != todo.ID {
		t.Fatalf("Todo completion = %v, want [%d]", gl.MarkedTodoIDs, todo.ID)
	}
}

func TestOrchestratorService_CompletionNoteFetchErrorLeavesTodoOpen(t *testing.T) {
	todo := Todo{ID: 3, Project: "group/project", MergeRequest: MergeRequest{IID: 103, State: "opened", WebURL: "http://gitlab.com/mr/103", Author: "author1"}}
	gl := &MockGitLabRepository{Todos: []Todo{todo}, Username: "bot", NotesByCall: [][]Note{{{ID: 4, Body: "## 審查結論\n需修改後再審"}}}, NoteErrors: []error{nil, errors.New("notes unavailable")}}
	worker := &Worker{Config: CollaboratorConfig{ID: "coder", Workspace: "/local/path"}, inputCh: make(chan WorkerTask, 1)}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, &WorkerManager{Workers: []*Worker{worker}})

	if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
		t.Fatal(err)
	}
	(<-worker.inputCh).OnSuccess("完成")
	if len(gl.MarkedTodoIDs) != 0 {
		t.Fatalf("Todo completed after note fetch error: %v", gl.MarkedTodoIDs)
	}
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
		inputCh: make(chan WorkerTask, 10),
	}

	wm := &WorkerManager{
		Workers: []*Worker{w},
	}

	service := NewOrchestratorService(nil, nil, wm)
	mr := MergeRequest{
		IID:    101,
		WebURL: "http://gitlab.com/mr/101",
	}

	service.assignToWorker("coder", mr, "/local/path", nil)

	select {
	case sent := <-w.inputCh:
		expected := "請閱讀最新的審查結論，於同一個 Merge Request 分支完成修正，並發表以「## 修正回覆」開頭的留言。Merge Request 101。網址為：http://gitlab.com/mr/101，請立刻處理\n"
		if sent.Text != expected {
			t.Errorf("預期發送為 '%s'，但得到 '%s'", expected, sent.Text)
		}
	default:
		t.Fatalf("預期有發送指令到 inputCh，但沒收到")
	}
}

func TestOrchestratorService_CoderTodoLifecycle(t *testing.T) {
	todo := Todo{
		ID:           1,
		Project:      "group/project",
		MergeRequest: MergeRequest{IID: 101, State: "opened", WebURL: "http://gitlab.com/mr/101", Author: "author1"},
	}

	newWorker := func() *Worker {
		return &Worker{Config: CollaboratorConfig{ID: "coder", Workspace: "/local/path"}, inputCh: make(chan WorkerTask, 1)}
	}

	t.Run("coder skips non-fix todo", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}, Notes: []Note{{ID: 1, Body: "## 審查結論\n可以合併"}}}
		worker := newWorker()
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, &WorkerManager{Workers: []*Worker{worker}})

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(gl.MarkedTodoIDs) != 1 || gl.MarkedTodoIDs[0] != todo.ID {
			t.Fatalf("Todo completion = %v, want [%d]", gl.MarkedTodoIDs, todo.ID)
		}
		select {
		case <-worker.inputCh:
			t.Fatal("coder received a non-fix todo")
		default:
		}
	})

	t.Run("coder skips a requested fix superseded by approval", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}, Notes: []Note{
			{ID: 1, Body: "## 審查結論\n需修改後再審"},
			{ID: 2, Body: "### 結論\n可以合併"},
		}}
		worker := newWorker()
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, &WorkerManager{Workers: []*Worker{worker}})

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(gl.MarkedTodoIDs) != 1 || gl.MarkedTodoIDs[0] != todo.ID {
			t.Fatalf("Todo completion = %v, want [%d]", gl.MarkedTodoIDs, todo.ID)
		}
		select {
		case <-worker.inputCh:
			t.Fatal("coder received a Todo superseded by approval")
		default:
		}
	})

	t.Run("coder dispatches requested-fix todo", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}, Username: "bot", Notes: []Note{{ID: 1, Body: "## 審查結論\n需修改後再審"}}}
		worker := newWorker()
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, &WorkerManager{Workers: []*Worker{worker}})

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(gl.MarkedTodoIDs) != 0 {
			t.Fatalf("Todo completed before worker success: %v", gl.MarkedTodoIDs)
		}
		select {
		case task := <-worker.inputCh:
			if task.OnSuccess == nil {
				t.Fatal("coder task has no completion callback")
			}
		default:
			t.Fatal("coder did not receive requested-fix todo")
		}
	})

	t.Run("completion requires new role-valid bot note", func(t *testing.T) {
		t.Run("leaves Todo open when a valid note is followed by an invalid note", func(t *testing.T) {
			gl := &MockGitLabRepository{Todos: []Todo{todo}, Username: "bot", NotesByCall: [][]Note{
				{{ID: 4, Body: "## 審查結論\n需修改後再審"}},
				{{ID: 4, Body: "## 審查結論\n需修改後再審"}, {ID: 5, Author: "bot", Body: "## 修正回覆\n已修正"}, {ID: 6, Author: "bot", Body: "## 其他留言"}},
			}}
			worker := newWorker()
			service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, &WorkerManager{Workers: []*Worker{worker}})
			if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
				t.Fatal(err)
			}
			(<-worker.inputCh).OnSuccess("完成")
			if len(gl.MarkedTodoIDs) != 0 {
				t.Fatalf("Todo completed despite newer invalid note: %v", gl.MarkedTodoIDs)
			}
		})

		t.Run("leaves Todo open for an invalid new note", func(t *testing.T) {
			gl := &MockGitLabRepository{Todos: []Todo{todo}, Username: "bot", NotesByCall: [][]Note{
				{{ID: 4, Body: "## 審查結論\n需修改後再審"}},
				{{ID: 4, Body: "## 審查結論\n需修改後再審"}, {ID: 5, Author: "bot", Body: "## 其他留言"}},
			}}
			worker := newWorker()
			service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, &WorkerManager{Workers: []*Worker{worker}})
			if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
				t.Fatal(err)
			}
			(<-worker.inputCh).OnSuccess("完成")
			if len(gl.MarkedTodoIDs) != 0 {
				t.Fatalf("Todo completed for invalid note: %v", gl.MarkedTodoIDs)
			}
		})

		t.Run("marks Todo for a new role-valid note", func(t *testing.T) {
			gl := &MockGitLabRepository{Todos: []Todo{todo}, Username: "bot", NotesByCall: [][]Note{
				{{ID: 4, Body: "## 審查結論\n需修改後再審"}},
				{{ID: 4, Body: "## 審查結論\n需修改後再審"}, {ID: 5, Author: "bot", Body: "## 修正回覆\n已修正"}},
			}}
			worker := newWorker()
			service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, &WorkerManager{Workers: []*Worker{worker}})
			if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil); err != nil {
				t.Fatal(err)
			}
			(<-worker.inputCh).OnSuccess("完成")
			if len(gl.MarkedTodoIDs) != 1 || gl.MarkedTodoIDs[0] != todo.ID {
				t.Fatalf("Todo completion = %v, want [%d]", gl.MarkedTodoIDs, todo.ID)
			}
		})
	})
}
