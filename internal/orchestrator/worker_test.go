package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockTerminal 實作 Terminal 介面用於測試
type MockTerminal struct {
	mu            sync.Mutex
	sessionActive bool
	sentKeys      []string
	lastEnv       []string
	history       []string
}

func (m *MockTerminal) Start(ctx context.Context, sessionID string, workspace string, cmd string, env []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionActive = true
	m.lastEnv = env
	return nil
}

func (m *MockTerminal) LastEnv() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastEnv
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
	// 回傳帶有提示符的畫面，以便 isPromptReady 判定為 ready
	return "Type your message\n>", nil
}

func (m *MockTerminal) CaptureHistory(sessionID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.history != nil {
		return append([]string(nil), m.history...), nil
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

// TestWorker_InjectsGitLabToken 驗證 collaborator 的 gitlab_token 會以 GITLAB_TOKEN
// 環境變數注入 CLI 進程，讓 CLI 在 workspace 內以該 token 的身分留言(而非退回讀
// glab 登入設定檔的使用者本人身分)。
func TestWorker_InjectsGitLabToken(t *testing.T) {
	const wantToken = "glpat-test-injection-token"
	cfg := CollaboratorConfig{
		ID:          "token-worker",
		Cmd:         "echo",
		Workspace:   "/tmp",
		GitLabToken: wantToken,
	}
	term := &MockTerminal{}
	w := NewWorker(cfg, "/tmp/logs", term)

	w.Start()
	time.Sleep(100 * time.Millisecond)
	w.Stop()

	found := false
	for _, e := range term.LastEnv() {
		if e == "GITLAB_TOKEN="+wantToken {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("預期 env 含 GITLAB_TOKEN=%s，實際未注入。env=%v", wantToken, term.LastEnv())
	}
}

// TestWorker_NoTokenNoInjection 驗證未設定 gitlab_token 時不會注入空的 GITLAB_TOKEN。
func TestWorker_NoTokenNoInjection(t *testing.T) {
	cfg := CollaboratorConfig{
		ID:        "no-token-worker",
		Cmd:       "echo",
		Workspace: "/tmp",
	}
	// 隔離真實 shell 環境的 GITLAB_TOKEN，避免 os.Environ() 複製進來干擾判斷
	if orig, had := os.LookupEnv("GITLAB_TOKEN"); had {
		os.Unsetenv("GITLAB_TOKEN")
		defer os.Setenv("GITLAB_TOKEN", orig)
	}

	term := &MockTerminal{}
	w := NewWorker(cfg, "/tmp/logs", term)

	w.Start()
	time.Sleep(100 * time.Millisecond)
	w.Stop()

	for _, e := range term.LastEnv() {
		if strings.HasPrefix(e, "GITLAB_TOKEN=") {
			t.Errorf("未設 gitlab_token 時不應注入 GITLAB_TOKEN，實際出現：%s", e)
		}
	}
}

func TestWorker_isPromptReady(t *testing.T) {
	w := &Worker{}
	cases := []struct {
		name   string
		screen string
		want   bool
	}{
		{"Claude 空提示符就緒", "❯ \n────\n  ⏵⏵ bypass permissions on (shift+tab to cycle)", true},
		{"Claude 提示符含建議文字就緒", "❯ Try \"write a test for repo.go\"\n────", true},
		{"傳統 > 提示符就緒", "Type your message\n>", true},
		{"思考中未就緒", "✢ Marinating… (2m · still thinking with medium effort)", false},
		{"bypass 首次接受畫面未就緒", "  WARNING: Claude Code running in Bypass Permissions mode\n  ❯ 1. No, exit\n    2. Yes, I accept\n  Enter to confirm · Esc to cancel", false},
		{"一般權限確認對話框未就緒", " Do you want to proceed?\n ❯ 1. Yes\n   3. No\n Esc to cancel · Tab to amend", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := w.isPromptReady(c.screen); got != c.want {
				t.Errorf("isPromptReady() = %v，預期 %v", got, c.want)
			}
		})
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

func TestWorker_ProcessAndSaveOutput_SkipsSessionLimitNoise(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := CollaboratorConfig{
		ID:        "reviewer",
		Workspace: "/tmp",
	}
	w := NewWorker(cfg, tmpDir, &MockTerminal{})

	lines := []string{
		"### 結論",
		"可以 merge ／ 請修正後再 merge",
		"```",
		"## 注意事項",
		"- 只使用 MCP 工具，不使用任何 shell 指令",
		"```",
		"⎿  You've hit your session limit · resets 6pm (Asia/Taipei)",
		"❯ 請開始評審 Merge Request 230。網址為：https://git.example.com/foo/bar/-/merge_requests/230",
		"⎿  You've hit your session limit · resets 6pm (Asia/Taipei)",
		"/usage-credits to request more usage from your admin.",
		"❯",
	}

	_, ok := w.processAndSaveOutputFromLines("reviewer", lines, "請開始評審 Merge Request 230。網址為：https://git.example.com/foo/bar/-/merge_requests/230")

	if ok {
		t.Fatalf("預期 session limit 輸出應判定為失敗")
	}

	answerPath := filepath.Join(tmpDir, "reviewer_answer.txt")
	if _, err := os.Stat(answerPath); !os.IsNotExist(err) {
		t.Fatalf("預期 session limit 輸出不應產生 answer 檔案，err=%v", err)
	}
}

func TestWorker_ProcessAndSaveOutput_KeepsValidReviewerAnswer(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := CollaboratorConfig{
		ID:        "reviewer",
		Workspace: "/tmp",
	}
	w := NewWorker(cfg, tmpDir, &MockTerminal{})

	lines := []string{
		"草稿分析",
		"─────",
		"### 結論",
		"可以 merge",
		"- 已確認主要風險可接受",
	}

	_, ok := w.processAndSaveOutputFromLines("reviewer", lines, "請開始評審 Merge Request 2。網址為：https://git.example.com/foo/bar/-/merge_requests/2")

	if !ok {
		t.Fatalf("預期正常 reviewer 回覆應判定為成功")
	}

	answerPath := filepath.Join(tmpDir, "reviewer_answer.txt")
	data, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatalf("預期正常 reviewer 回覆應寫入 answer 檔案: %v", err)
	}

	got := strings.TrimSpace(string(data))
	want := "### 結論\n可以 merge\n- 已確認主要風險可接受"
	if got != want {
		t.Fatalf("answer 內容不符\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestWorker_ProcessAndSaveOutput_IgnoresOldSessionLimitHistory(t *testing.T) {
	tmpDir := t.TempDir()
	term := &MockTerminal{
		history: []string{
			"⎿  You've hit your session limit · resets 6pm (Asia/Taipei)",
			"/usage-credits to request more usage from your admin.",
			"❯",
			"草稿分析",
			"─────",
			"### 結論",
			"可以 merge",
		},
	}
	cfg := CollaboratorConfig{
		ID:        "reviewer",
		Workspace: "/tmp",
	}
	w := NewWorker(cfg, tmpDir, term)

	_, ok := w.processAndSaveOutput("reviewer", 3, "請開始評審 Merge Request 2。網址為：https://git.example.com/foo/bar/-/merge_requests/2")

	if !ok {
		t.Fatalf("舊歷史中的 session limit 不應讓本次正常回覆判定失敗")
	}

	answerPath := filepath.Join(tmpDir, "reviewer_answer.txt")
	data, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatalf("舊歷史中的 session limit 不應阻止本次正常回覆寫檔: %v", err)
	}

	got := strings.TrimSpace(string(data))
	want := "### 結論\n可以 merge"
	if got != want {
		t.Fatalf("answer 內容不符\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
