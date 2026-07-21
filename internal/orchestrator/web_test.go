package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestWebServerHidesAgentToken(t *testing.T) {
	path := t.TempDir() + "/settings.yaml"
	if err := SaveWorkflowSettings(path, WorkflowSettings{Agents: []CollaboratorConfig{{
		ID: "coder", GitLabToken: "gitlab-secret",
	}}}); err != nil {
		t.Fatal(err)
	}
	h := NewWebServer(path, NewWorkerManager(nil, t.TempDir(), &MockTerminal{}), nil)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", r.Code, r.Body.String())
	}
	for _, secret := range []string{"gitlab-secret"} {
		if strings.Contains(r.Body.String(), secret) {
			t.Fatalf("body leaked %q: %s", secret, r.Body.String())
		}
	}
}

func TestWebServerDeletesAgentAndContainer(t *testing.T) {
	path := t.TempDir() + "/settings.yaml"
	if err := SaveWorkflowSettings(path, WorkflowSettings{Agents: []CollaboratorConfig{{ID: "agent-a", Cmd: "codex", Workspace: t.TempDir(), GitLabToken: "token"}}}); err != nil {
		t.Fatal(err)
	}
	workers := NewWorkerManager([]CollaboratorConfig{{ID: "agent-a", Cmd: "codex", Workspace: t.TempDir()}}, t.TempDir(), &MockTerminal{})
	h := NewWebServer(path, workers, nil)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodDelete, "/api/agents/agent-a", nil))
	if r.Code != http.StatusNoContent {
		t.Fatalf("code=%d body=%s", r.Code, r.Body.String())
	}
	settings, err := LoadWorkflowSettings(path)
	if err != nil || len(settings.Agents) != 0 {
		t.Fatalf("settings=%#v err=%v", settings, err)
	}
}

func TestWebServerAddsAgent(t *testing.T) {
	path := t.TempDir() + "/settings.yaml"
	workers := NewWorkerManager(nil, t.TempDir(), &MockTerminal{})
	h := NewWebServer(path, workers, nil)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(`{"id":"coder","cmd":"echo","workspace":"/tmp","gitlab_token":"secret"}`)))
	if r.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", r.Code, r.Body.String())
	}
	settings, err := LoadWorkflowSettings(path)
	if err != nil || len(settings.Agents) != 1 || settings.Agents[0].ID != "coder" || !reflect.DeepEqual(settings.Agents[0].Skills, []string{"superpowers:senior-coder-workflow"}) {
		t.Fatalf("settings=%#v err=%v", settings, err)
	}
}

func TestWebServerAddsReviewerWithReviewSkill(t *testing.T) {
	path := t.TempDir() + "/settings.yaml"
	workers := NewWorkerManager(nil, t.TempDir(), &MockTerminal{})
	h := NewWebServer(path, workers, nil)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(`{"id":"reviewer","cmd":"agy","workspace":"/tmp","gitlab_token":"secret"}`)))
	if r.Code != http.StatusCreated {
		t.Fatalf("code=%d body=%s", r.Code, r.Body.String())
	}
	settings, err := LoadWorkflowSettings(path)
	if err != nil || !reflect.DeepEqual(settings.Agents[0].Skills, []string{"superpowers:git-mr-workflow-reviewer"}) {
		t.Fatalf("settings=%#v err=%v", settings, err)
	}
}

func TestDashboardLoadsStoredSettings(t *testing.T) {
	page, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"fetch('/api/settings')", "name=\"skills\"", "data.skills.split(',')", "name=\"prompt_suffix\"", "name=\"check_ci_success\"", "name=\"allowed_projects\"", "name=\"allowed_mr_authors\""} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}

func TestWebServerRestartsAgent(t *testing.T) {
	workers := NewWorkerManager(nil, t.TempDir(), &MockTerminal{})
	if err := workers.AddAndStart(CollaboratorConfig{ID: "coder", Cmd: "echo", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(workers.StopAll)
	workers.Find("coder").Stop()

	h := NewWebServer(t.TempDir()+"/settings.yaml", workers, nil)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/agents/restart?id=coder", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", r.Code, r.Body.String())
	}
}
