package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type MockTaskDispatcher struct {
	DispatchedTasks []DispatchTaskInput
	BusyMap         map[string]bool
	DispatchErr     error
	IsBusyErr       error
}

func (m *MockTaskDispatcher) DispatchTask(ctx context.Context, input DispatchTaskInput) error {
	if m.DispatchErr != nil {
		return m.DispatchErr
	}
	m.DispatchedTasks = append(m.DispatchedTasks, input)
	return nil
}

func (m *MockTaskDispatcher) IsBusy(ctx context.Context, agentID string) (bool, error) {
	if m.IsBusyErr != nil {
		return false, m.IsBusyErr
	}
	if m.BusyMap != nil {
		return m.BusyMap[agentID], nil
	}
	return false, nil
}

func TestNewCaoDispatcher_Defaults(t *testing.T) {
	dispatcher := NewCaoDispatcher("", "", "")
	if dispatcher.CaoBinPath != "cao" {
		t.Errorf("期望預設 CaoBinPath 為 cao，但得到 %s", dispatcher.CaoBinPath)
	}
	if dispatcher.SessionName != "cao-main" {
		t.Errorf("期望預設 SessionName 為 cao-main，但得到 %s", dispatcher.SessionName)
	}
	if dispatcher.ServerURL != "http://localhost:9889" {
		t.Errorf("期望預設 ServerURL 為 http://localhost:9889，但得到 %s", dispatcher.ServerURL)
	}
}

func TestCaoDispatcher_HTTP(t *testing.T) {
	t.Run("DispatchTask via HTTP Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/sessions/cao-main/terminals" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`[{"id": "term-1"}]`))
				return
			}
			if r.URL.Path == "/terminals/term-1/input" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"success": true}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		dispatcher := NewCaoDispatcher("invalid-cli-path", "cao-main", server.URL)
		err := dispatcher.DispatchTask(context.Background(), DispatchTaskInput{
			Instruction: "測試任務",
			Workspace:   "/workspace",
		})
		if err != nil {
			t.Fatalf("期望 HTTP 派發成功，但得到錯誤: %v", err)
		}
	})

	t.Run("IsBusy via HTTP Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"sessions": [{"status": "running"}]}`))
		}))
		defer server.Close()

		dispatcher := NewCaoDispatcher("invalid-cli-path", "cao-main", server.URL)
		busy, err := dispatcher.IsBusy(context.Background(), "reviewer")
		if err != nil {
			t.Fatalf("期望 HTTP 狀態檢查成功，但得到錯誤: %v", err)
		}
		if !busy {
			t.Errorf("期望 busy 為 true，但得到 false")
		}
	})
}
