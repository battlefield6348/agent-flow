package orchestrator

import (
	"context"
	"errors"
	"strings"
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
	Path  string
	Err   error
	Calls int
}

func (m *MockWorkspaceRepository) FindLocalPath(ctx context.Context, projectPath string) (string, error) {
	m.Calls++
	return m.Path, m.Err
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

	err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"}, "", "")
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

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"}, "", "")
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

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"}, "", "")
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

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"}, "", "")
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

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil, "", ""); err != nil {
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

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil, "", ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("coder expected 1 task, got %d", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("coder dispatches mentioned todo without conclusion header", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}, Username: "bot", Notes: []Note{{ID: 1, Body: "@coder 請協助重構此模組"}}}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, dispatcher)

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil, "", ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("coder expected 1 task for direct mention, got %d", len(dispatcher.DispatchedTasks))
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
		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil, "", "")
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
		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("期望指派 1 個任務，但得到了 %d 個", len(dispatcher.DispatchedTasks))
		}
		task := dispatcher.DispatchedTasks[0]
		if task.AgentID != "reviewer" || task.MRIID != 200 || task.Workspace != "" {
			t.Errorf("派發任務內容不符合期望: %+v", task)
		}
	})
}

func TestOrchestratorService_DuplicateMRTodos(t *testing.T) {
	todo1 := Todo{ID: 101, Project: "group/proj", MergeRequest: MergeRequest{IID: 278, State: "opened", WebURL: "http://gitlab.com/mr/278", Author: "author1"}}
	todo2 := Todo{ID: 102, Project: "group/proj", MergeRequest: MergeRequest{IID: 278, State: "opened", WebURL: "http://gitlab.com/mr/278", Author: "author1"}}

	t.Run("duplicate MR todo deduplication on CI check failure", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo1, todo2},
			Pipelines: []Pipeline{
				{ID: 1, Status: "running"},
			},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil, "", "")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("Expected 0 dispatched tasks due to running CI, got %d", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("duplicate MR todo deduplication and mark done after dispatch", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo1, todo2},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil, "", "")
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("Expected 1 dispatched task for duplicate MRs, got %d", len(dispatcher.DispatchedTasks))
		}
		if len(gl.MarkedTodoIDs) != 2 {
			t.Fatalf("Expected 2 todo IDs marked as done, got %d (%v)", len(gl.MarkedTodoIDs), gl.MarkedTodoIDs)
		}
	})
}

func TestOrchestratorService_CIFailureDirectToCoder(t *testing.T) {
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

	t.Run("CI is failed, coder receives repair task", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo},
			Pipelines: []Pipeline{
				{ID: 10, Status: "failed"},
			},
			Notes: []Note{}, // 沒有「需修改後再審」之類的評論
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil, "", "")
		if err != nil {
			t.Fatalf("ScanAndAssignForAgent failed: %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("Expected coder to be assigned 1 repair task on CI failure, got %d", len(dispatcher.DispatchedTasks))
		}
		task := dispatcher.DispatchedTasks[0]
		if task.AgentID != "coder" {
			t.Errorf("Expected agentID coder, got %s", task.AgentID)
		}
		if !strings.Contains(task.Instruction, "CI") || !strings.Contains(task.Instruction, "修正") {
			t.Errorf("Instruction should mention CI repair, got: %s", task.Instruction)
		}
	})

	t.Run("CI is failed, reviewer skips assignment", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo},
			Pipelines: []Pipeline{
				{ID: 10, Status: "failed"},
			},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil, "", "")
		if err != nil {
			t.Fatalf("ScanAndAssignForAgent failed: %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("Expected reviewer to skip task on CI failure, got %d", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("CI is running, both coder and reviewer skip assignment", func(t *testing.T) {
		gl := &MockGitLabRepository{
			Todos: []Todo{todo},
			Pipelines: []Pipeline{
				{ID: 10, Status: "running"},
			},
		}
		ws := &MockWorkspaceRepository{Path: "/local/path"}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)
		service.SetCheckCISuccess(true)

		err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil, "", "")
		if err != nil {
			t.Fatalf("ScanAndAssignForAgent failed: %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("Expected coder to skip task on running CI, got %d", len(dispatcher.DispatchedTasks))
		}

		err = service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil, "", "")
		if err != nil {
			t.Fatalf("ScanAndAssignForAgent failed: %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("Expected reviewer to skip task on running CI, got %d", len(dispatcher.DispatchedTasks))
		}
	})
}

func TestOrchestratorService_LocalWorkspaceByAgent(t *testing.T) {
	todo := Todo{ID: 1, Project: "group/project", MergeRequest: MergeRequest{IID: 23, State: "opened", WebURL: "http://gitlab.com/mr/23", Author: "author1"}}

	t.Run("reviewer 僅透過 GitLab 審查，不需本機 Repo 也能派發", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}}
		ws := &MockWorkspaceRepository{Err: errors.New("local workspace not found")}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)

		if err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, nil, nil, "", ""); err != nil {
			t.Fatal(err)
		}
		if ws.Calls != 0 {
			t.Errorf("reviewer 不應查找本機 Repo，但查找了 %d 次", ws.Calls)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("期望 reviewer 派發 1 個任務，但得到 %d 個", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("coder 找不到本機 Repo 時不派發", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}}
		ws := &MockWorkspaceRepository{Err: errors.New("local workspace not found")}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil, "", ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("coder 找不到本機 Repo 時不應派發，但派發了 %d 個", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("coder 的派發指令包含本機 Repo 路徑", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{todo}}
		ws := &MockWorkspaceRepository{Path: "/home/agent/projects/project"}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)

		if err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, nil, nil, "", ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("期望 coder 派發 1 個任務，但得到 %d 個", len(dispatcher.DispatchedTasks))
		}
		task := dispatcher.DispatchedTasks[0]
		if task.Workspace != "/home/agent/projects/project" {
			t.Errorf("Workspace = %q，期望為本機 Repo 路徑", task.Workspace)
		}
		if !strings.Contains(task.Instruction, "/home/agent/projects/project") {
			t.Errorf("coder 指令應包含本機 Repo 路徑，但得到: %s", task.Instruction)
		}
	})
}
