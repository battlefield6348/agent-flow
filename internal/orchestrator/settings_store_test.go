package orchestrator

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestSettingsStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	want := WorkflowSettings{
		GitLabURL:        "https://gitlab.example.com",
		IntervalSeconds:  60,
		CheckCISuccess:   true,
		AllowedProjects:  []string{"group/project"},
		AllowedMRAuthors: []string{"author"},
		Agents: []CollaboratorConfig{{
			ID: "coder", Cmd: "codex", GitLabToken: "gitlab-secret",
		}},
	}
	if err := SaveWorkflowSettings(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWorkflowSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.GitLabURL != want.GitLabURL || got.IntervalSeconds != want.IntervalSeconds || got.CheckCISuccess != want.CheckCISuccess || !reflect.DeepEqual(got.AllowedProjects, want.AllowedProjects) || !reflect.DeepEqual(got.AllowedMRAuthors, want.AllowedMRAuthors) || len(got.Agents) != 1 || got.Agents[0].ID != "coder" || got.Agents[0].GitLabToken != "gitlab-secret" {
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
