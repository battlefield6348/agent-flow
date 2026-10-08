package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"reflect"
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

	err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, []string{"group/project"}, []string{"author1"}, "")
	if err != nil {
		t.Fatalf("ScanAndAssign failed: %v", err)
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

		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, []string{"group/project"}, []string{"author1"}, "")
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

		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, []string{"group/project"}, []string{"author1"}, "")
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

		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, []string{"group/project"}, []string{"author1"}, "")
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

		if err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "coder"}, gl, nil, nil, ""); err != nil {
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

		if err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "coder"}, gl, nil, nil, ""); err != nil {
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

		if err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "coder"}, gl, nil, nil, ""); err != nil {
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
		dispatcher := &MockTaskDispatcher{BusyMap: map[string]bool{"cao-gitlab-reviewer": true}}
		service := NewOrchestratorServiceWithDispatcher(gl, ws, dispatcher)
		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, nil, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("busy 時不應指派任務，但指派了 %d 個", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("idle dispatcher assigns task", func(t *testing.T) {
		dispatcher := &MockTaskDispatcher{BusyMap: map[string]bool{"cao-gitlab-reviewer": false}}
		service := NewOrchestratorServiceWithDispatcher(gl, ws, dispatcher)
		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, nil, nil, "")
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

		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, nil, nil, "")
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

		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, nil, nil, "")
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

		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "coder"}, gl, nil, nil, "")
		if err != nil {
			t.Fatalf("ScanAndAssign failed: %v", err)
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

		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, nil, nil, "")
		if err != nil {
			t.Fatalf("ScanAndAssign failed: %v", err)
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

		err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "coder"}, gl, nil, nil, "")
		if err != nil {
			t.Fatalf("ScanAndAssign failed: %v", err)
		}
		if len(dispatcher.DispatchedTasks) != 0 {
			t.Fatalf("Expected coder to skip task on running CI, got %d", len(dispatcher.DispatchedTasks))
		}

		err = service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, nil, nil, "")
		if err != nil {
			t.Fatalf("ScanAndAssign failed: %v", err)
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

		if err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "reviewer"}, gl, nil, nil, ""); err != nil {
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

		if err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "coder"}, gl, nil, nil, ""); err != nil {
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

		if err := service.ScanAndAssign(context.Background(), CollaboratorConfig{ID: "coder"}, gl, nil, nil, ""); err != nil {
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

func reviewerPool(replicas int) CollaboratorConfig {
	return CollaboratorConfig{ID: "reviewer", Role: RoleReviewer, CaoSessionName: "gitlab-reviewer", Replicas: replicas}
}

func mrTodo(id, iid int) Todo {
	return Todo{ID: id, Project: "group/proj", MergeRequest: MergeRequest{IID: iid, State: "opened", WebURL: fmt.Sprintf("http://gitlab.com/mr/%d", iid), Author: "author1"}}
}

func TestOrchestratorService_SessionPool(t *testing.T) {
	t.Run("多張 MR 分流至不同的閒置 session", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{mrTodo(1, 101), mrTodo(2, 102), mrTodo(3, 103)}}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{}, dispatcher)

		if err := service.ScanAndAssign(context.Background(), reviewerPool(3), gl, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		got := map[string]int{}
		for _, task := range dispatcher.DispatchedTasks {
			got[task.CaoSessionName] = task.MRIID
		}
		want := map[string]int{"cao-gitlab-reviewer-1": 101, "cao-gitlab-reviewer-2": 102, "cao-gitlab-reviewer-3": 103}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("派發分布 = %v, want %v", got, want)
		}
	})

	t.Run("忙碌中的 session 不會被派發", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{mrTodo(1, 101)}}
		dispatcher := &MockTaskDispatcher{BusyMap: map[string]bool{"cao-gitlab-reviewer-1": true}}
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{}, dispatcher)

		if err := service.ScanAndAssign(context.Background(), reviewerPool(2), gl, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 || dispatcher.DispatchedTasks[0].CaoSessionName != "cao-gitlab-reviewer-2" {
			t.Fatalf("期望派發至 cao-gitlab-reviewer-2，但得到 %+v", dispatcher.DispatchedTasks)
		}
	})

	t.Run("pool 全忙時剩餘 Todo 保留至下一輪", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{mrTodo(1, 101), mrTodo(2, 102), mrTodo(3, 103)}}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, &MockWorkspaceRepository{}, dispatcher)

		if err := service.ScanAndAssign(context.Background(), reviewerPool(2), gl, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 2 {
			t.Fatalf("期望派發 2 個任務，但得到 %d 個", len(dispatcher.DispatchedTasks))
		}
		if !reflect.DeepEqual(gl.MarkedTodoIDs, []int{1, 2}) {
			t.Errorf("僅已派發的 Todo 應標記完成，得到 %v", gl.MarkedTodoIDs)
		}
	})

	t.Run("審查中的 MR 有新 Todo 時延後，避免兩個 session 同時審同一張", func(t *testing.T) {
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(nil, &MockWorkspaceRepository{}, dispatcher)
		col := reviewerPool(2)

		first := &MockGitLabRepository{Todos: []Todo{mrTodo(1, 101)}}
		if err := service.ScanAndAssign(context.Background(), col, first, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		dispatcher.BusyMap = map[string]bool{dispatcher.DispatchedTasks[0].CaoSessionName: true}

		second := &MockGitLabRepository{Todos: []Todo{mrTodo(2, 101)}}
		if err := service.ScanAndAssign(context.Background(), col, second, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("審查中的 MR 不應再派發，但共派發 %d 次", len(dispatcher.DispatchedTasks))
		}
		if len(second.MarkedTodoIDs) != 0 {
			t.Errorf("延後的 Todo 不應標記完成，得到 %v", second.MarkedTodoIDs)
		}

		dispatcher.BusyMap = nil
		if err := service.ScanAndAssign(context.Background(), col, second, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 2 {
			t.Fatalf("前一次審查結束後應重新派發，但共派發 %d 次", len(dispatcher.DispatchedTasks))
		}
	})

	t.Run("依 role 判斷角色而非 id", func(t *testing.T) {
		gl := &MockGitLabRepository{Todos: []Todo{mrTodo(1, 101)}}
		ws := &MockWorkspaceRepository{Err: errors.New("local workspace not found")}
		dispatcher := &MockTaskDispatcher{}
		service := NewOrchestratorService(gl, ws, dispatcher)

		col := CollaboratorConfig{ID: "reviewer-b", Role: RoleReviewer}
		if err := service.ScanAndAssign(context.Background(), col, gl, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		if len(dispatcher.DispatchedTasks) != 1 {
			t.Fatalf("期望派發 1 個任務，但得到 %d 個", len(dispatcher.DispatchedTasks))
		}
		task := dispatcher.DispatchedTasks[0]
		if !strings.Contains(task.Instruction, "評審") {
			t.Errorf("reviewer 角色應收到評審指令，但得到: %s", task.Instruction)
		}
		if task.AgentID != "reviewer-b" || task.CaoSessionName != "cao-gitlab-reviewer-b" {
			t.Errorf("派發目標不符: %+v", task)
		}
	})
}
