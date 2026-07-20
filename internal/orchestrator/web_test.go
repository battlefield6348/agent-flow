package orchestrator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebServerHidesAgentToken(t *testing.T) {
	path := t.TempDir() + "/settings.yaml"
	if err := SaveWorkflowSettings(path, WorkflowSettings{Agents: []CollaboratorConfig{{ID: "coder", GitLabToken: "secret"}}}); err != nil {
		t.Fatal(err)
	}
	h := NewWebServer(path, NewWorkerManager(nil, t.TempDir(), &MockTerminal{}), nil)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if r.Code != http.StatusOK || strings.Contains(r.Body.String(), "secret") {
		t.Fatalf("code=%d body=%s", r.Code, r.Body.String())
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
	if err != nil || len(settings.Agents) != 1 || settings.Agents[0].ID != "coder" {
		t.Fatalf("settings=%#v err=%v", settings, err)
	}
}
