package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"
)

type MockGitLabRepository struct {
	Todos       []Todo
	Pipelines   []Pipeline
	Notes       []Note
	Err         error
	DoneTodoIDs []int
}

func (m *MockGitLabRepository) FetchPendingTodos(ctx context.Context) ([]Todo, error) {
	return m.Todos, m.Err
}
func (m *MockGitLabRepository) MarkTodoAsDone(ctx context.Context, todoID int) error {
	m.DoneTodoIDs = append(m.DoneTodoIDs, todoID)
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

func TestOrchestratorService_DoesNotMarkTodoDoneImmediatelyWhenAssigned(t *testing.T) {
	gl := &MockGitLabRepository{
		Todos: []Todo{
			{
				ID:      42,
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
	workerManager := &WorkerManager{
		Workers: []*Worker{
			NewWorker(CollaboratorConfig{
				ID:        "reviewer",
				Workspace: "/local/path",
			}, t.TempDir(), &MockTerminal{}),
		},
	}

	service := NewOrchestratorService(gl, ws, workerManager)

	err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssignForAgent failed: %v", err)
	}

	if len(gl.DoneTodoIDs) != 0 {
		t.Fatalf("預期派工當下不標記 todo done，實際 done IDs: %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_HandleWorkerSuccess_MarksDoneAfterNewBotNoteAppears(t *testing.T) {
	gl := &MockGitLabRepository{
		Notes: []Note{
			{ID: 10, Author: "mockuser", Body: "old"},
			{ID: 11, Author: "someone-else", Body: "other"},
			{ID: 12, Author: "mockuser", Body: "## 審查結論\n需修改後再審"},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{}, nil)

	err := service.handleWorkerSuccess(context.Background(), gl, "reviewer", 42, "group/project", MergeRequest{IID: 101}, "mockuser", 10)
	if err != nil {
		t.Fatalf("handleWorkerSuccess failed: %v", err)
	}

	if len(gl.DoneTodoIDs) != 1 || gl.DoneTodoIDs[0] != 42 {
		t.Fatalf("預期 todo 42 被標記 done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_HandleWorkerSuccess_DoesNotMarkDoneWithoutNewBotNote(t *testing.T) {
	gl := &MockGitLabRepository{
		Notes: []Note{
			{ID: 10, Author: "mockuser", Body: "old"},
			{ID: 11, Author: "someone-else", Body: "other"},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{}, nil)

	err := service.handleWorkerSuccess(context.Background(), gl, "reviewer", 42, "group/project", MergeRequest{IID: 101}, "mockuser", 10)
	if err == nil {
		t.Fatalf("預期沒有新 bot 留言時回傳錯誤")
	}
	if len(gl.DoneTodoIDs) != 0 {
		t.Fatalf("沒有新 bot 留言時不應標記 todo done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_HandleWorkerSuccess_DoesNotMarkDoneForTranscriptLikeBotNote(t *testing.T) {
	gl := &MockGitLabRepository{
		Notes: []Note{
			{ID: 10, Author: "mockuser", Body: "old"},
			{ID: 12, Author: "mockuser", Body: "› 請開始評審 Merge Request 32\n• Ran git status"},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{}, nil)

	err := service.handleWorkerSuccess(context.Background(), gl, "reviewer", 42, "group/project", MergeRequest{IID: 101}, "mockuser", 10)
	if err == nil {
		t.Fatalf("預期 transcript 樣式留言不應被視為有效完成")
	}
	if len(gl.DoneTodoIDs) != 0 {
		t.Fatalf("transcript 樣式留言不應標記 todo done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestBuildWorkerInstruction_ReviewerRequiresDirectPosting(t *testing.T) {
	mr := MergeRequest{IID: 32, WebURL: "https://git.example.com/group/project/-/merge_requests/32"}
	got := buildWorkerInstruction("reviewer", mr)

	if !strings.Contains(got, "請直接在 GitLab 的 Merge Request 留言") {
		t.Fatalf("reviewer 指令應要求直接留言，got=%q", got)
	}
	if !strings.Contains(got, "留言完成後，再把相同的最終審查內容輸出到終端") {
		t.Fatalf("reviewer 指令應要求留言後再輸出最終審查內容，got=%q", got)
	}
	if strings.Contains(got, "不要直接在 GitLab 或 Merge Request 留言") {
		t.Fatalf("reviewer 指令不應再禁止直接留言，got=%q", got)
	}
}

func TestBuildWorkerInstruction_CoderAddressesReview(t *testing.T) {
	mr := MergeRequest{IID: 32, WebURL: "https://git.example.com/group/project/-/merge_requests/32"}
	got := buildWorkerInstruction("coder", mr)

	if !strings.Contains(got, "## 審查結論") {
		t.Fatalf("coder 指令應要求讀取以「## 審查結論」開頭的審查留言，got=%q", got)
	}
	if !strings.Contains(got, "同一個來源分支") {
		t.Fatalf("coder 指令應要求 push 回同一來源分支，got=%q", got)
	}
	if !strings.Contains(got, "## 修正回覆") {
		t.Fatalf("coder 指令應要求留下以「## 修正回覆」開頭的留言，got=%q", got)
	}
}

func TestOrchestratorService_HandleWorkerSuccess_CoderMarksDoneOnFixReplyNote(t *testing.T) {
	gl := &MockGitLabRepository{
		Notes: []Note{
			{ID: 10, Author: "mockuser", Body: "## 審查結論\n需修改後再審"},
			{ID: 13, Author: "mockuser", Body: "## 修正回覆\n- 已修正 foo.go:12"},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{}, nil)

	err := service.handleWorkerSuccess(context.Background(), gl, "coder", 42, "group/project", MergeRequest{IID: 101}, "mockuser", 10)
	if err != nil {
		t.Fatalf("coder handleWorkerSuccess failed: %v", err)
	}
	if len(gl.DoneTodoIDs) != 1 || gl.DoneTodoIDs[0] != 42 {
		t.Fatalf("預期 coder 修正回覆後 todo 42 被標記 done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_HandleWorkerSuccess_CoderRejectsReviewFormatNote(t *testing.T) {
	// coder 若只貼出審查格式（## 審查結論）而非修正回覆，不應被視為完成。
	gl := &MockGitLabRepository{
		Notes: []Note{
			{ID: 12, Author: "mockuser", Body: "## 審查結論\n需修改後再審"},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{}, nil)

	err := service.handleWorkerSuccess(context.Background(), gl, "coder", 42, "group/project", MergeRequest{IID: 101}, "mockuser", 10)
	if err == nil {
		t.Fatalf("預期 coder 貼審查格式留言不應被視為有效完成")
	}
	if len(gl.DoneTodoIDs) != 0 {
		t.Fatalf("coder 未貼修正回覆時不應標記 todo done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_CoderGate_MarksDoneWhenNoReviewerConclusion(t *testing.T) {
	// coder todo 但 MR 上沒有任何「## 審查結論」留言（例如他人 @ 提及）→ 直接標 done，不指派。
	gl := &MockGitLabRepository{
		Todos: []Todo{
			{ID: 7, Project: "group/project", MergeRequest: MergeRequest{IID: 101, State: "opened", Author: "author1"}},
		},
		Notes: []Note{
			{ID: 1, Author: "someone", Body: "隨口留言，不是審查"},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, nil)

	err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssignForAgent failed: %v", err)
	}
	if len(gl.DoneTodoIDs) != 1 || gl.DoneTodoIDs[0] != 7 {
		t.Fatalf("預期無審查結論時 coder todo 被標 done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_CoderGate_ProceedsWhenReviewerConclusionExists(t *testing.T) {
	// coder todo 且 MR 已有「## 審查結論」留言 → 守門放行（此處 workerManager 為 nil 走 mock 模式，不標 done）。
	gl := &MockGitLabRepository{
		Todos: []Todo{
			{ID: 8, Project: "group/project", MergeRequest: MergeRequest{IID: 101, State: "opened", Author: "author1"}},
		},
		Notes: []Note{
			{ID: 1, Author: "bot", Body: "## 審查結論\n🔧 需修改後再審：詳見必修問題"},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, nil)

	err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssignForAgent failed: %v", err)
	}
	if len(gl.DoneTodoIDs) != 0 {
		t.Fatalf("有審查結論時 coder 守門應放行、不應在守門階段標 done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_CoderGate_MarksDoneWhenReviewApprovedNoFix(t *testing.T) {
	// coder todo 落在「已審查但結論為核准（無必修問題）」的 MR 上：守門應標 done，不指派，
	// 否則 coder 無事可修 → 產不出「## 修正回覆」→ 每輪重跑。
	gl := &MockGitLabRepository{
		Todos: []Todo{
			{ID: 9, Project: "group/project", MergeRequest: MergeRequest{IID: 101, State: "opened", Author: "author1"}},
		},
		Notes: []Note{
			{ID: 1, Author: "bot", Body: "## 審查結論\n✅ 核准：變更正確、測試充分，可合併。\n\n## 必修問題\n無"},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, nil)

	err := service.ScanAndAssignForAgent(context.Background(), "coder", gl, []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssignForAgent failed: %v", err)
	}
	if len(gl.DoneTodoIDs) != 1 || gl.DoneTodoIDs[0] != 9 {
		t.Fatalf("預期核准（無需修改）時 coder todo 被標 done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_ReviewerDedupe_MarksDoneWhenLatestReviewStillMatchesCurrentDiff(t *testing.T) {
	base := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	gl := &MockGitLabRepository{
		Todos: []Todo{
			{ID: 10, Project: "group/project", MergeRequest: MergeRequest{IID: 101, State: "opened", Author: "author1"}},
		},
		Notes: []Note{
			{ID: 1, System: true, Body: "added 1 commit", CreatedAt: base},
			{ID: 2, Author: "mockuser", Body: "## 審查結論\n✅ 核准", CreatedAt: base.Add(1 * time.Minute)},
			{ID: 3, Author: "author1", Body: "@mockuser", CreatedAt: base.Add(2 * time.Minute)},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, nil)

	err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssignForAgent failed: %v", err)
	}
	if len(gl.DoneTodoIDs) != 1 || gl.DoneTodoIDs[0] != 10 {
		t.Fatalf("預期同一 diff 的重複 reviewer todo 被標 done，實際 %v", gl.DoneTodoIDs)
	}
}

func TestOrchestratorService_ReviewerDedupe_AllowsRereviewAfterNewCommit(t *testing.T) {
	base := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	gl := &MockGitLabRepository{
		Todos: []Todo{
			{ID: 11, Project: "group/project", MergeRequest: MergeRequest{IID: 101, State: "opened", Author: "author1"}},
		},
		Notes: []Note{
			{ID: 1, System: true, Body: "added 1 commit", CreatedAt: base},
			{ID: 2, Author: "mockuser", Body: "## 審查結論\n🔧 需修改後再審", CreatedAt: base.Add(1 * time.Minute)},
			{ID: 3, System: true, Body: "added 1 commit", CreatedAt: base.Add(2 * time.Minute)},
			{ID: 4, Author: "author1", Body: "@mockuser", CreatedAt: base.Add(3 * time.Minute)},
		},
	}
	service := NewOrchestratorService(gl, &MockWorkspaceRepository{Path: "/local/path"}, nil)

	err := service.ScanAndAssignForAgent(context.Background(), "reviewer", gl, []string{"group/project"}, []string{"author1"})
	if err != nil {
		t.Fatalf("ScanAndAssignForAgent failed: %v", err)
	}
	if len(gl.DoneTodoIDs) != 0 {
		t.Fatalf("預期新 commit 後允許 re-review，不應在守門階段標 done，實際 %v", gl.DoneTodoIDs)
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

	service.assignToWorker("coder", mr, "/local/path", "", nil)

	select {
	case sent := <-w.inputCh:
		expected := "請處理 Merge Request 101 上的最新審查意見。網址為：http://gitlab.com/mr/101\n請讀取該 MR 上最新一則以「## 審查結論」開頭的審查留言，逐項修正其中的必修問題，並將修正 push 到該 MR 的同一個來源分支（不要另開新分支或新 MR）。完成後，在該 MR 留一則以「## 修正回覆」開頭的留言，逐項說明每條必修問題如何處理；不要貼思考過程、操作 transcript、狀態列或工具輸出。留言完成後，再把相同的最終內容輸出到終端。\n"
		if sent.Text != expected {
			t.Errorf("預期發送為 '%s'，但得到 '%s'", expected, sent.Text)
		}
	default:
		t.Fatalf("預期有發送指令到 inputCh，但沒收到")
	}
}
