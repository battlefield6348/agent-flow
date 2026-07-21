package orchestrator

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// MockTerminal 實作 Terminal 介面用於測試
type MockTerminal struct {
	mu            sync.Mutex
	sessionActive bool
	sentKeys      []string
	startedEnv    []string
	started       chan struct{}
	taskStarted   bool
	captureCount  int
}

func (m *MockTerminal) Start(ctx context.Context, sessionID string, workspace string, cmd string, env []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionActive = true
	m.startedEnv = append([]string(nil), env...)
	if m.started != nil {
		close(m.started)
		m.started = nil
	}
	return nil
}

func (m *MockTerminal) StartedEnv() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.startedEnv...)
}

func (m *MockTerminal) Stop(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionActive = false
	return nil
}

func (m *MockTerminal) SendKeys(sessionID string, keys string, enter bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentKeys = append(m.sentKeys, keys)
	if keys == "任務" {
		m.taskStarted = true
		m.captureCount = 0
	}
	return nil
}

func (m *MockTerminal) GetSentKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	// 複製 slice 避免併發衝突
	res := make([]string, len(m.sentKeys))
	copy(res, m.sentKeys)
	return res
}

func (m *MockTerminal) CapturePane(sessionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.taskStarted {
		m.captureCount++
		if m.captureCount == 1 {
			return "working", nil
		}
		return "完成\nType your message\n>", nil
	}
	// 回傳帶有提示符的畫面，以便 isPromptReady 判定為 ready
	return "Type your message\n>", nil
}

func (m *MockTerminal) CaptureHistory(sessionID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.taskStarted {
		return []string{">", "完成輸出"}, nil
	}
	return []string{">"}, nil
}

func (m *MockTerminal) HasSession(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionActive
}

func (m *MockTerminal) IsPaneDead(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.sessionActive
}

func TestWorker_LifecycleSafety(t *testing.T) {
	cfg := CollaboratorConfig{
		ID:        "test-worker",
		Cmd:       "echo",
		Workspace: "/tmp",
	}
	term := &MockTerminal{}
	w := NewWorker(cfg, "/tmp/logs", term)

	// 1. 測試重複調用 Start 是否會造成多個 goroutine 洩漏或異常
	w.Start()
	w.Start() // 重複啟動

	time.Sleep(100 * time.Millisecond)

	// 2. 測試重複調用 Stop 是否會引發 panic (close of closed channel)
	w.Stop()

	// 若無保護，此處將會引發 panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Worker.Stop() panicked on repeated calls: %v", r)
		}
	}()
	w.Stop() // 重複關閉
}

func TestWorkerPassesOnlyConfiguredTokensToTerminal(t *testing.T) {
	terminal := &MockTerminal{started: make(chan struct{})}
	worker := NewWorker(CollaboratorConfig{
		ID: "agent-a", Cmd: "codex", Workspace: t.TempDir(),
		GitLabToken: "gitlab-a",
	}, t.TempDir(), terminal)
	worker.Start()
	defer worker.Stop()

	select {
	case <-terminal.started:
	case <-time.After(time.Second):
		t.Fatal("terminal did not start")
	}

	got := terminal.StartedEnv()
	want := []string{"TERM=screen-256color", "GITLAB_TOKEN=gitlab-a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}

func TestTaskCompletionRequiresPaneChange(t *testing.T) {
	if taskCompletionReady(false, 2, ">") {
		t.Fatal("unchanged ready prompt must not complete a newly sent task")
	}
	if !taskCompletionReady(true, 2, ">") {
		t.Fatal("changed and stable ready prompt must complete the task")
	}
}

func TestWorkerTaskOnSuccess(t *testing.T) {
	terminal := &MockTerminal{started: make(chan struct{})}
	worker := NewWorker(CollaboratorConfig{ID: "worker", Cmd: "codex", Workspace: t.TempDir()}, t.TempDir(), terminal)
	started := terminal.started
	worker.Start()
	defer worker.Stop()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("terminal did not start")
	}

	called := make(chan string, 1)
	worker.SendTask(WorkerTask{Text: "任務", OnSuccess: func(output string) { called <- output }})

	select {
	case output := <-called:
		if output != "完成輸出" {
			t.Fatalf("OnSuccess output = %q, want %q", output, "完成輸出")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("OnSuccess was not called after task completion")
	}
}

func TestWorkerTaskOnSuccessForDuplicateOutput(t *testing.T) {
	terminal := &MockTerminal{started: make(chan struct{})}
	worker := NewWorker(CollaboratorConfig{ID: "worker", Cmd: "codex", Workspace: t.TempDir()}, t.TempDir(), terminal)
	started := terminal.started
	worker.Start()
	defer worker.Stop()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("terminal did not start")
	}

	first := make(chan string, 1)
	second := make(chan string, 1)
	worker.SendTask(WorkerTask{Text: "任務", OnSuccess: func(output string) { first <- output }})
	worker.SendTask(WorkerTask{Text: "任務", OnSuccess: func(output string) { second <- output }})

	for _, called := range []chan string{first, second} {
		select {
		case output := <-called:
			if output != "完成輸出" {
				t.Fatalf("OnSuccess output = %q, want %q", output, "完成輸出")
			}
		case <-time.After(8 * time.Second):
			t.Fatal("OnSuccess was not called after task completion")
		}
	}
}

func TestWorker_BuildPromptMsg(t *testing.T) {
	t.Run("預設無後綴情況", func(t *testing.T) {
		cfg := CollaboratorConfig{
			ID:           "reviewer",
			PromptSuffix: "",
		}
		w := &Worker{Config: cfg}
		msg := w.BuildPromptMsg("reviewer")
		expected := "請待命，等候我給予你具體的 Merge Request 評審任務。"
		if msg != expected {
			t.Errorf("預期為 '%s'，但得到 '%s'", expected, msg)
		}
	})

	t.Run("設定後綴情況", func(t *testing.T) {
		cfg := CollaboratorConfig{
			ID:           "reviewer",
			PromptSuffix: " 並且在評審完成後標記 @ryan",
		}
		w := &Worker{Config: cfg}
		msg := w.BuildPromptMsg("reviewer")
		expected := "請待命，等候我給予你具體的 Merge Request 評審任務。 並且在評審完成後標記 @ryan"
		if msg != expected {
			t.Errorf("預期為 '%s'，但得到 '%s'", expected, msg)
		}
	})

	t.Run("不同 sessionID 測試", func(t *testing.T) {
		cfg := CollaboratorConfig{
			ID:           "coder",
			PromptSuffix: "，請立刻處理",
		}
		w := &Worker{Config: cfg}
		msg := w.BuildPromptMsg("coder")
		expected := "請待命，等候我給予你具體的開發與修正任務。，請立刻處理"
		if msg != expected {
			t.Errorf("預期為 '%s'，但得到 '%s'", expected, msg)
		}
	})
}

func TestWorker_GetSkillPrefix(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		cmd      string
		expected string
	}{
		{
			name:     "Coder 與 codex 命令",
			id:       "coder",
			cmd:      "codex",
			expected: "$",
		},
		{
			name:     "Reviewer 與 agy 命令",
			id:       "reviewer",
			cmd:      "agy",
			expected: "/",
		},
		{
			name:     "Reviewer 與絕對路徑 agy 命令",
			id:       "reviewer",
			cmd:      "/home/user/.local/bin/agy",
			expected: "/",
		},
		{
			name:     "使用 antigravity 名稱的命令",
			id:       "reviewer",
			cmd:      "antigravity",
			expected: "/",
		},
		{
			name:     "使用包含 antigravity 的絕對路徑命令",
			id:       "reviewer",
			cmd:      "/home/user/.local/bin/antigravity",
			expected: "/",
		},
		{
			name:     "Coder 與絕對路徑 codex 命令",
			id:       "coder",
			cmd:      "/home/user/.nvm/versions/node/v24.13.0/bin/codex",
			expected: "$",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Worker{
				Config: CollaboratorConfig{
					ID:  tt.id,
					Cmd: tt.cmd,
				},
			}
			actual := w.GetSkillPrefix()
			if actual != tt.expected {
				t.Errorf("GetSkillPrefix() = %q, expected %q", actual, tt.expected)
			}
		})
	}
}
