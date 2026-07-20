package orchestrator

import (
	"testing"
	"time"
)

func TestWorkerManagerAddAndStartExposesStatus(t *testing.T) {
	m := NewWorkerManager(nil, t.TempDir(), &MockTerminal{})
	if err := m.AddAndStart(CollaboratorConfig{ID: "coder", Cmd: "echo", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.StopAll)

	got := m.Statuses()
	if len(got) != 1 || got[0].ID != "coder" || got[0].Busy {
		t.Fatalf("got %#v", got)
	}
	if err := m.AddAndStart(CollaboratorConfig{ID: "coder", Cmd: "echo"}); err == nil {
		t.Fatal("expected duplicate ID error")
	}
}

func TestWorkerManagerRestartStartsStoppedWorker(t *testing.T) {
	m := NewWorkerManager(nil, t.TempDir(), &MockTerminal{})
	if err := m.AddAndStart(CollaboratorConfig{ID: "coder", Cmd: "echo", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.StopAll)
	m.Find("coder").Stop()

	if err := m.Restart("coder"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !m.Find("coder").IsRunning() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !m.Find("coder").IsRunning() {
		t.Fatal("worker was not restarted")
	}
}
