package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func (m *MockTaskDispatcher) IsBusy(ctx context.Context, sessionName string) (bool, error) {
	if m.IsBusyErr != nil {
		return false, m.IsBusyErr
	}
	if m.BusyMap != nil {
		return m.BusyMap[sessionName], nil
	}
	return false, nil
}

func (m *MockTaskDispatcher) EnsureSessions(ctx context.Context, agents []CollaboratorConfig) error {
	return nil
}

func (m *MockTaskDispatcher) ShutdownSessions(ctx context.Context) error {
	return nil
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

	t.Run("IsBusy via HTTP Processing Status Success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if r.URL.Path == "/sessions/cao-gitlab-reviewer" {
				_, _ = w.Write([]byte(`{"session":{"id":"cao-gitlab-reviewer"},"terminals":[{"id":"4fdca9de","status":"processing"}]}`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		dispatcher := NewCaoDispatcher("invalid-cli-path", "cao-main", server.URL)
		dispatcher.CheckTmuxPrompt = nil
		busy, err := dispatcher.IsBusy(context.Background(), "cao-gitlab-reviewer")
		if err != nil {
			t.Fatalf("期望 HTTP 狀態檢查成功，但得到錯誤: %v", err)
		}
		if !busy {
			t.Errorf("期望 processing 狀態時 busy 為 true，但得到 false")
		}
	})

	t.Run("IsBusy via HTTP Processing But Tmux Prompt Ready", func(t *testing.T) {
		deleted := false
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				deleted = true
			}
			w.WriteHeader(http.StatusOK)
			if r.URL.Path == "/sessions/cao-gitlab-reviewer" {
				_, _ = w.Write([]byte(`{"session":{"id":"cao-gitlab-reviewer"},"terminals":[{"id":"4fdca9de","status":"processing"}]}`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		}))
		defer server.Close()

		dispatcher := NewCaoDispatcher("invalid-cli-path", "cao-main", server.URL)
		dispatcher.CheckTmuxPrompt = func(ctx context.Context, sessionName string) (bool, bool) {
			return true, true
		}
		busy, err := dispatcher.IsBusy(context.Background(), "cao-gitlab-reviewer")
		if err != nil {
			t.Fatalf("期望 HTTP 狀態檢查成功，但得到錯誤: %v", err)
		}
		if busy {
			t.Errorf("期望 tmux prompt ready 時 busy 為 false，但得到 true")
		}
		if deleted {
			t.Errorf("期望判定閒置時不刪除仍在運作的 Session 紀錄，但送出了 DELETE 請求")
		}
	})

	t.Run("IsBusy via HTTP Fallback Processing", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if r.URL.Path == "/sessions" {
				_, _ = w.Write([]byte(`[{"id": "cao-gitlab-reviewer", "status": "processing"}]`))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		dispatcher := NewCaoDispatcher("invalid-cli-path", "cao-main", server.URL)
		dispatcher.CheckTmuxPrompt = nil
		busy, err := dispatcher.IsBusy(context.Background(), "cao-gitlab-reviewer")
		if err != nil {
			t.Fatalf("期望 HTTP 狀態檢查成功，但得到錯誤: %v", err)
		}
		if !busy {
			t.Errorf("期望 fallback processing 狀態時 busy 為 true，但得到 false")
		}
	})
}

func TestCaoDispatcher_EnsureSessions(t *testing.T) {
	dispatcher := NewCaoDispatcher("true", "cao-main", "")
	agents := []CollaboratorConfig{
		{
			ID:          "reviewer",
			GitLabToken: "glpat-reviewer-test-token",
		},
	}
	err := dispatcher.EnsureSessions(context.Background(), agents)
	if err != nil {
		t.Fatalf("EnsureSessions 執行失敗: %v", err)
	}
}

func TestCaoDispatcher_AutoRelaunchOnMissingSession(t *testing.T) {
	// 驗證當 dispatch 遭遇 session missing 錯誤時，能自動呼叫 EnsureSessions 並 retry 派發
	relaunched := false
	dispatcher := NewCaoDispatcher("true", "cao-main", "")
	dispatcher.ServerURL = ""
	dispatcher.agents = []CollaboratorConfig{
		{
			ID:             "reviewer",
			CaoSessionName: "gitlab-reviewer",
		},
	}
	dispatcher.LauncherFunc = func(ctx context.Context, agent CollaboratorConfig) error {
		relaunched = true
		return nil
	}

	callCount := 0
	dispatcher.CLISendFunc = func(ctx context.Context, session, instruction, workspace string) (string, error) {
		callCount++
		if callCount == 1 {
			return "", fmt.Errorf("未檢測到運作中的 CAO Session (cao-gitlab-reviewer)")
		}
		return "success", nil
	}

	err := dispatcher.DispatchTask(context.Background(), DispatchTaskInput{
		AgentID:        "reviewer",
		CaoSessionName: "gitlab-reviewer",
		Instruction:    "測試自動重啟派發",
	})

	if err != nil {
		t.Fatalf("期望自動重啟後 retry 成功，但得到錯誤: %v", err)
	}
	if !relaunched {
		t.Errorf("期望啟動重啟 callback (LauncherFunc)，但未被呼叫")
	}
	if callCount != 2 {
		t.Errorf("期望執行 2 次 send 嘗試，但實際上執行了 %d 次", callCount)
	}
}

func TestCaoDispatcher_EnsureSessionsMergesAgents(t *testing.T) {
	// 驗證逐一代理人呼叫 EnsureSessions 時，不會覆蓋掉先前登記的其他代理人設定
	dispatcher := NewCaoDispatcher("true", "cao-main", "")
	dispatcher.LauncherFunc = func(ctx context.Context, agent CollaboratorConfig) error { return nil }

	ctx := context.Background()
	_ = dispatcher.EnsureSessions(ctx, []CollaboratorConfig{{ID: "reviewer", CaoSessionName: "gitlab-reviewer", CaoAgentProfile: "review_supervisor"}})
	_ = dispatcher.EnsureSessions(ctx, []CollaboratorConfig{{ID: "coder", CaoSessionName: "gitlab-coder"}})
	_ = dispatcher.EnsureSessions(ctx, []CollaboratorConfig{{ID: "reviewer", CaoSessionName: "gitlab-reviewer", CaoAgentProfile: "custom_profile"}})

	if len(dispatcher.agents) != 2 {
		t.Fatalf("期望保留 2 個代理人設定，但實際為 %d 個", len(dispatcher.agents))
	}
	reviewer, found := dispatcher.findAgentConfig("reviewer", "")
	if !found || reviewer.CaoAgentProfile != "custom_profile" {
		t.Errorf("期望 reviewer 設定被更新為 custom_profile，但得到 %+v (found=%v)", reviewer, found)
	}
	if _, found := dispatcher.findAgentConfig("coder", ""); !found {
		t.Errorf("期望 coder 設定仍存在，但找不到")
	}
}

func TestCaoDispatcher_IsBusyChecksRequestedSession(t *testing.T) {
	// 預設 Session (cao-main) 閒置不代表 pool 中指定的 Session 也閒置
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sessions/cao-main":
			_, _ = w.Write([]byte(`{"session":{"id":"cao-main"},"terminals":[{"id":"t0","status":"idle"}]}`))
		case "/sessions/cao-gitlab-reviewer-2":
			_, _ = w.Write([]byte(`{"session":{"id":"cao-gitlab-reviewer-2"},"terminals":[{"id":"t2","status":"processing"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dispatcher := NewCaoDispatcher("invalid-cli-path", "cao-main", server.URL)
	dispatcher.CheckTmuxPrompt = nil
	busy, err := dispatcher.IsBusy(context.Background(), "cao-gitlab-reviewer-2")
	if err != nil {
		t.Fatal(err)
	}
	if !busy {
		t.Errorf("期望 cao-gitlab-reviewer-2 為忙碌，但得到閒置")
	}
}

func TestCaoDispatcher_EnsureSessionsLaunchesReplicas(t *testing.T) {
	dispatcher := NewCaoDispatcher("true", "cao-main", "")
	var launched []string
	dispatcher.LauncherFunc = func(ctx context.Context, agent CollaboratorConfig) error {
		launched = append(launched, agent.CaoSessionName)
		return nil
	}

	agents := []CollaboratorConfig{{ID: "reviewer", CaoSessionName: "gitlab-reviewer", Replicas: 3}}
	if err := dispatcher.EnsureSessions(context.Background(), agents); err != nil {
		t.Fatal(err)
	}
	want := []string{"cao-gitlab-reviewer-1", "cao-gitlab-reviewer-2", "cao-gitlab-reviewer-3"}
	if !reflect.DeepEqual(launched, want) {
		t.Errorf("啟動的 Session = %v, want %v", launched, want)
	}
}

func TestCaoDispatcher_RelaunchUsesReplicaSessionName(t *testing.T) {
	dispatcher := NewCaoDispatcher("true", "cao-main", "")
	dispatcher.ServerURL = ""
	dispatcher.agents = []CollaboratorConfig{{ID: "reviewer", CaoSessionName: "gitlab-reviewer", Replicas: 3, CaoAgentProfile: "review_supervisor"}}

	var relaunched CollaboratorConfig
	dispatcher.LauncherFunc = func(ctx context.Context, agent CollaboratorConfig) error {
		relaunched = agent
		return nil
	}
	callCount := 0
	dispatcher.CLISendFunc = func(ctx context.Context, session, instruction, workspace string) (string, error) {
		callCount++
		if callCount == 1 {
			return "", fmt.Errorf("未檢測到運作中的 CAO Session (%s)", session)
		}
		return "success", nil
	}

	err := dispatcher.DispatchTask(context.Background(), DispatchTaskInput{
		AgentID:        "reviewer",
		CaoSessionName: "cao-gitlab-reviewer-2",
		Instruction:    "測試",
	})
	if err != nil {
		t.Fatal(err)
	}
	if relaunched.CaoSessionName != "cao-gitlab-reviewer-2" || relaunched.CaoAgentProfile != "review_supervisor" {
		t.Errorf("應以原設定重啟 cao-gitlab-reviewer-2，但得到 %+v", relaunched)
	}
}

func TestSessionListed(t *testing.T) {
	list := "cao-gitlab-reviewer-10  idle\ncao-gitlab-coder  processing\n"
	if sessionListed(list, "cao-gitlab-reviewer-1") {
		t.Error("cao-gitlab-reviewer-1 不應因前綴相同而被視為已啟動")
	}
	if !sessionListed(list, "cao-gitlab-reviewer-10") || !sessionListed(list, "cao-gitlab-coder") {
		t.Error("已列出的 Session 應被視為已啟動")
	}
}
