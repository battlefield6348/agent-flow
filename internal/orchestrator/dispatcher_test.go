package orchestrator

import (
	"context"
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
	dispatcher := NewCaoDispatcher("", "")
	if dispatcher.CaoBinPath != "cao" {
		t.Errorf("期望預設 CaoBinPath 為 cao，但得到 %s", dispatcher.CaoBinPath)
	}
	if dispatcher.SessionName != "cao-main" {
		t.Errorf("期望預設 SessionName 為 cao-main，但得到 %s", dispatcher.SessionName)
	}
}
