package orchestrator

import (
	"embed"
	"encoding/json"
	"net/http"
	"sync"
)

//go:embed web/index.html
var webFiles embed.FS

type WebServer struct {
	settingsPath string
	workers      *WorkerManager
	scheduler    *Scheduler
	mu           sync.Mutex
}

func NewWebServer(settingsPath string, workers *WorkerManager, scheduler *Scheduler) http.Handler {
	s := &WebServer{settingsPath: settingsPath, workers: workers, scheduler: scheduler}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("GET /api/agents", s.getAgents)
	mux.HandleFunc("POST /api/agents", s.addAgent)
	mux.HandleFunc("POST /api/agents/restart", s.restartAgent)
	return mux
}

func (s *WebServer) index(w http.ResponseWriter, r *http.Request) {
	data, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *WebServer) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := LoadWorkflowSettings(s.settingsPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *WebServer) putSettings(w http.ResponseWriter, r *http.Request) {
	var settings WorkflowSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if settings.GitLabURL == "" || settings.IntervalSeconds <= 0 {
		http.Error(w, "gitlab_url and interval_seconds are required", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	current, err := LoadWorkflowSettings(s.settingsPath)
	if err == nil {
		settings.Agents = current.Agents
		err = SaveWorkflowSettings(s.settingsPath, settings)
	}
	s.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.scheduler != nil {
		s.scheduler.Update(settings)
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *WebServer) getAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.workers.Statuses())
}

func (s *WebServer) addAgent(w http.ResponseWriter, r *http.Request) {
	var agent CollaboratorConfig
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if agent.ID == "" || agent.Cmd == "" || agent.Workspace == "" || agent.GitLabToken == "" {
		http.Error(w, "id, cmd, workspace, and gitlab_token are required", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	settings, err := LoadWorkflowSettings(s.settingsPath)
	if err == nil {
		for _, existing := range settings.Agents {
			if existing.ID == agent.ID {
				err = errDuplicateAgent
			}
		}
	}
	if err == nil {
		settings.Agents = append(settings.Agents, agent)
		err = SaveWorkflowSettings(s.settingsPath, settings)
	}
	s.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err := s.workers.AddAndStart(agent); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.scheduler != nil {
		if err := s.scheduler.StartAgent(agent); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	for _, status := range s.workers.Statuses() {
		if status.ID == agent.ID {
			writeJSON(w, http.StatusCreated, status)
			return
		}
	}
	w.WriteHeader(http.StatusCreated)
}

func (s *WebServer) restartAgent(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "agent id is required", http.StatusBadRequest)
		return
	}
	if err := s.workers.Restart(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	for _, status := range s.workers.Statuses() {
		if status.ID == id {
			writeJSON(w, http.StatusOK, status)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

var errDuplicateAgent = &duplicateAgentError{}

type duplicateAgentError struct{}

func (*duplicateAgentError) Error() string { return "agent already exists" }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
