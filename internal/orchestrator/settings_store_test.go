package orchestrator

import (
	"path/filepath"
	"testing"
)

func TestSettingsStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	want := WorkflowSettings{
		GitLabURL:       "https://gitlab.example.com",
		IntervalSeconds: 60,
		Agents:          []CollaboratorConfig{{ID: "coder", Cmd: "codex", GitLabToken: "secret"}},
	}
	if err := SaveWorkflowSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWorkflowSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.GitLabURL != want.GitLabURL || got.IntervalSeconds != want.IntervalSeconds || len(got.Agents) != 1 || got.Agents[0].ID != "coder" || got.Agents[0].GitLabToken != "secret" {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestLoadWorkflowSettingsMissingFileIsEmpty(t *testing.T) {
	got, err := LoadWorkflowSettings(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if got.GitLabURL != "" || got.IntervalSeconds != 0 || len(got.Agents) != 0 {
		t.Fatalf("got %#v", got)
	}
}
