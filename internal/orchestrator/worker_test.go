package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"
)

// MockTerminal 實作 Terminal 介面用於測試
type MockTerminal struct {
	mu            sync.Mutex
	sessionActive bool
}

func (m *MockTerminal) Start(ctx context.Context, sessionID string, workspace string, cmd string, env []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionActive = true
	return nil
}

func (m *MockTerminal) Stop(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionActive = false
	return nil
}

func (m *MockTerminal) SendKeys(sessionID string, keys string, enter bool) error {
	return nil
}

func (m *MockTerminal) CapturePane(sessionID string) (string, error) {
	// 回傳帶有提示符的畫面，以便 isPromptReady 判定為 ready
	return "Type your message\n>", nil
}

func (m *MockTerminal) CaptureHistory(sessionID string) ([]string, error) {
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
