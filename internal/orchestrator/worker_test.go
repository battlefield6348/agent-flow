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
