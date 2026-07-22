package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// DispatchTaskInput 封裝發送給 Agent 的任務資訊
type DispatchTaskInput struct {
	AgentID     string
	Workspace   string
	Instruction string
	MRIID       int
	MRWebURL    string
}

// TaskDispatcher 定義與 Agent 派發工具互動的介面
type TaskDispatcher interface {
	DispatchTask(ctx context.Context, input DispatchTaskInput) error
	IsBusy(ctx context.Context, agentID string) (bool, error)
}

// CaoDispatcher 實現與 cli-agent-orchestrator (cao) 的整合介面，支援 HTTP API 與 CLI 雙模式
type CaoDispatcher struct {
	CaoBinPath  string
	SessionName string
	ServerURL   string
	HTTPClient  *http.Client
}

func NewCaoDispatcher(caoBinPath, sessionName, serverURL string) *CaoDispatcher {
	if caoBinPath == "" {
		caoBinPath = "cao"
	}
	if sessionName == "" {
		sessionName = "cao-main"
	}
	if serverURL == "" {
		serverURL = "http://localhost:9889"
	}
	return &CaoDispatcher{
		CaoBinPath:  caoBinPath,
		SessionName: sessionName,
		ServerURL:   strings.TrimSuffix(serverURL, "/"),
		HTTPClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *CaoDispatcher) DispatchTask(ctx context.Context, input DispatchTaskInput) error {
	if c.ServerURL != "" {
		err := c.dispatchViaHTTP(ctx, input)
		if err == nil {
			slog.Info("成功透過 cao-server HTTP API 派發任務", "session", c.SessionName)
			return nil
		}
		slog.Warn("透過 cao-server HTTP API 派發失敗，降級使用 CLI 執行", "error", err)
	}

	return c.dispatchViaCLI(ctx, input)
}

func (c *CaoDispatcher) dispatchViaHTTP(ctx context.Context, input DispatchTaskInput) error {
	url := fmt.Sprintf("%s/sessions/%s/send", c.ServerURL, c.SessionName)
	payload := map[string]string{
		"message":   input.Instruction,
		"workspace": input.Workspace,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
}

func (c *CaoDispatcher) dispatchViaCLI(ctx context.Context, input DispatchTaskInput) error {
	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "send", c.SessionName, input.Instruction)
	if input.Workspace != "" {
		cmd.Dir = input.Workspace
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cao session send 失敗: %w, 輸出: %s", err, string(out))
	}
	return nil
}

func (c *CaoDispatcher) IsBusy(ctx context.Context, agentID string) (bool, error) {
	if c.ServerURL != "" {
		busy, err := c.isBusyViaHTTP(ctx, agentID)
		if err == nil {
			return busy, nil
		}
		slog.Warn("透過 cao-server 檢查狀態失敗，降級使用 CLI 檢查", "error", err)
	}

	return c.isBusyViaCLI(ctx, agentID)
}

func (c *CaoDispatcher) isBusyViaHTTP(ctx context.Context, agentID string) (bool, error) {
	url := fmt.Sprintf("%s/sessions", c.ServerURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	return strings.Contains(string(respBody), "running"), nil
}

func (c *CaoDispatcher) isBusyViaCLI(ctx context.Context, agentID string) (bool, error) {
	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("cao session list 失敗: %w, 輸出: %s", err, string(out))
	}
	return strings.Contains(string(out), "running"), nil
}
