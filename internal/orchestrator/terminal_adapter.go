package orchestrator

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

type TmuxTerminal struct{}

func NewTmuxTerminal() *TmuxTerminal {
	return &TmuxTerminal{}
}

func (t *TmuxTerminal) Start(ctx context.Context, sessionID string, workspace string, cmdStr string, env []string) error {
	slog.Debug("Terminal Start", "session_id", sessionID, "workspace", workspace, "cmd", cmdStr)
	// 先殺掉舊的 session
	_ = t.Stop(sessionID)

	var startCmd *exec.Cmd
	if workspace != "" {
		_ = os.MkdirAll(workspace, 0755)
		startCmd = exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", sessionID, "-c", workspace, "sh", "-c", cmdStr)
	} else {
		startCmd = exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", sessionID, "sh", "-c", cmdStr)
	}
	startCmd.Env = env

	if err := startCmd.Run(); err != nil {
		return err
	}
	
	_ = exec.Command("tmux", "set-option", "-t", sessionID, "remain-on-exit", "on").Run()
	return nil
}

func (t *TmuxTerminal) Stop(sessionID string) error {
	return exec.Command("tmux", "kill-session", "-t", sessionID).Run()
}

func (t *TmuxTerminal) SendKeys(sessionID string, keys string, enter bool) error {
	slog.Debug("Terminal SendKeys", "session_id", sessionID, "keys", keys, "enter", enter)
	err := exec.Command("tmux", "send-keys", "-t", sessionID, "-l", keys).Run()
	if err != nil {
		return err
	}
	if enter {
		return exec.Command("tmux", "send-keys", "-t", sessionID, "C-m").Run()
	}
	return nil
}

func (t *TmuxTerminal) CapturePane(sessionID string) (string, error) {
	out, err := exec.Command("tmux", "capture-pane", "-pt", sessionID).Output()
	return string(out), err
}

func (t *TmuxTerminal) CaptureHistory(sessionID string) ([]string, error) {
	cmd := exec.Command("tmux", "capture-pane", "-S", "-", "-J", "-p", "-t", sessionID)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	rawLines := strings.Split(string(out), "\n")

	// 去除底部填充空白行
	end := len(rawLines)
	for end > 0 && strings.TrimSpace(rawLines[end-1]) == "" {
		end--
	}

	return rawLines[:end], nil
}

func (t *TmuxTerminal) HasSession(sessionID string) bool {
	checkCmd := exec.Command("tmux", "has-session", "-t", sessionID)
	return checkCmd.Run() == nil
}

func (t *TmuxTerminal) IsPaneDead(sessionID string) bool {
	cmd := exec.Command("tmux", "display-message", "-p", "-F", "#{pane_dead}", "-t", sessionID)
	out, err := cmd.Output()
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(out)) == "1"
}
