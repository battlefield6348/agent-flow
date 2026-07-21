package orchestrator

import (
	"strings"
	"testing"
)

func TestMergeTerminalEnvironmentPreservesHostAuthenticationEnvironment(t *testing.T) {
	merged := mergeTerminalEnvironment(
		[]string{"HOME=/host/home", "PATH=/host/bin", "GITLAB_TOKEN=old"},
		[]string{"TERM=screen-256color", "GITLAB_TOKEN=agent-token"},
	)
	joined := strings.Join(merged, "\n")
	for _, want := range []string{"HOME=/host/home", "PATH=/host/bin", "TERM=screen-256color", "GITLAB_TOKEN=agent-token"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment missing %q: %v", want, merged)
		}
	}
	if strings.Contains(joined, "GITLAB_TOKEN=old") {
		t.Fatalf("old token was not overridden: %v", merged)
	}
}

func TestTmuxSessionArgsScopesGitLabTokenToAgentSession(t *testing.T) {
	args := tmuxSessionArgs("coder", "/workspace/coder", "codex", []string{"TERM=screen-256color", "GITLAB_TOKEN=coder-token"})
	joined := strings.Join(args, "\n")
	for _, want := range []string{"-s\nagent-coder", "-c\n/workspace/coder", "-e\nGITLAB_TOKEN=coder-token", "codex"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tmux arguments missing %q: %v", want, args)
		}
	}
}
