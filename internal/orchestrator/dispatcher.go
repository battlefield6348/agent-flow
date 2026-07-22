package orchestrator

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
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

// CaoDispatcher 實現與 cli-agent-orchestrator (cao) 的整合介面
type CaoDispatcher struct {
	CaoBinPath  string
	SessionName string
}

func NewCaoDispatcher(caoBinPath, sessionName string) *CaoDispatcher {
	if caoBinPath == "" {
		caoBinPath = "cao"
	}
	if sessionName == "" {
		sessionName = "cao-main"
	}
	return &CaoDispatcher{
		CaoBinPath:  caoBinPath,
		SessionName: sessionName,
	}
}

func (c *CaoDispatcher) DispatchTask(ctx context.Context, input DispatchTaskInput) error {
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
	cmd := exec.CommandContext(ctx, c.CaoBinPath, "session", "list")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("cao session list 失敗: %w, 輸出: %s", err, string(out))
	}
	return strings.Contains(string(out), "running"), nil
}
