package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestSchedulerStartAgentRejectsDuplicate(t *testing.T) {
	s := NewScheduler(nil, time.Hour, nil, nil, nil, "https://gitlab.example.com")
	s.Start(context.Background())
	if err := s.StartAgent(CollaboratorConfig{ID: "coder", GitLabToken: "token"}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartAgent(CollaboratorConfig{ID: "coder", GitLabToken: "token"}); err == nil {
		t.Fatal("expected duplicate agent error")
	}
}
