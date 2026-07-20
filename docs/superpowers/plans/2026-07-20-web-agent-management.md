# Web Agent Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a localhost Web page that displays agent runtime state and persists newly added agents so they join worker scheduling immediately and survive restarts.

**Architecture:** `data/agents.yaml` becomes the runtime agent source of truth. It imports legacy YAML collaborators only on its first creation. A synchronized worker manager and scheduler accept dynamic agents; a Go `net/http` page polls JSON every two seconds.

**Tech Stack:** Go standard library (`net/http`, `embed`, `sync`), existing `gopkg.in/yaml.v3`, tmux adapter.

---

## File Structure

- Create: `internal/orchestrator/agent_store.go` and `agent_store_test.go` — YAML persistence/import with atomic writes.
- Modify: `internal/orchestrator/worker.go`, `service.go`, and `worker_test.go` — synchronized dynamic worker management and state snapshots.
- Modify: `internal/orchestrator/scheduler.go`; create `scheduler_test.go` — immediate polling registration per agent.
- Create: `internal/orchestrator/web.go`, `web_test.go`, and `web/index.html` — localhost API and no-build UI.
- Modify: `cmd/agent-flow/main.go`, `internal/orchestrator/config.go`, `config_test.go`, `README.md`, `Makefile`, and `configs/config.yaml.example` — startup wiring and operator docs.

### Task 1: Persist runtime agents

**Files:**
- Create: `internal/orchestrator/agent_store.go`
- Test: `internal/orchestrator/agent_store_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestAgentStoreImportsLegacyCollaboratorsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "agents.yaml")
	legacy := []CollaboratorConfig{{ID: "coder", Cmd: "codex", GitLabToken: "token"}}

	got, err := LoadAgentStore(path, legacy)
	if err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(got, legacy) { t.Fatalf("got %#v", got) }

	got, err = LoadAgentStore(path, []CollaboratorConfig{{ID: "reviewer"}})
	if err != nil { t.Fatal(err) }
	if got[0].ID != "coder" { t.Fatalf("got %q", got[0].ID) }
}

func TestAgentStoreSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.yaml")
	want := []CollaboratorConfig{{ID: "reviewer", Cmd: "agy", GitLabToken: "secret"}}
	if err := SaveAgentStore(path, want); err != nil { t.Fatal(err) }
	got, err := LoadAgentStore(path, nil)
	if err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(got, want) { t.Fatalf("got %#v", got) }
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/orchestrator -run TestAgentStore -count=1`

Expected: FAIL because `LoadAgentStore` and `SaveAgentStore` do not exist.

- [ ] **Step 3: Implement the minimum store**

```go
type agentStore struct {
	Agents []CollaboratorConfig `yaml:"agents"`
}

func LoadAgentStore(path string, legacy []CollaboratorConfig) ([]CollaboratorConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := SaveAgentStore(path, legacy); err != nil { return nil, err }
		return legacy, nil
	}
	if err != nil { return nil, err }
	var store agentStore
	if err := yaml.Unmarshal(data, &store); err != nil { return nil, err }
	return store.Agents, nil
}
```

Implement `SaveAgentStore` with `os.MkdirAll`, `os.CreateTemp`, mode `0600`, close, then `os.Rename`. This avoids partial token files and adds no dependency.

- [ ] **Step 4: Verify GREEN**

Run: `go test ./internal/orchestrator -run TestAgentStore -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/agent_store.go internal/orchestrator/agent_store_test.go
git commit -m "feat: persist runtime agents"
```

### Task 2: Safely add and observe workers

**Files:**
- Modify: `internal/orchestrator/worker.go`
- Modify: `internal/orchestrator/service.go`
- Test: `internal/orchestrator/worker_test.go`

- [ ] **Step 1: Write the failing manager test**

```go
func TestWorkerManagerAddAndStartExposesIdleStatus(t *testing.T) {
	m := NewWorkerManager(nil, t.TempDir(), &MockTerminal{})
	if err := m.AddAndStart(CollaboratorConfig{ID: "coder", Cmd: "echo", Workspace: t.TempDir()}); err != nil { t.Fatal(err) }
	t.Cleanup(m.StopAll)

	statuses := m.Statuses()
	if len(statuses) != 1 || statuses[0].ID != "coder" || !statuses[0].Running || statuses[0].Busy {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
	if err := m.AddAndStart(CollaboratorConfig{ID: "coder", Cmd: "echo"}); err == nil {
		t.Fatal("expected duplicate ID error")
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/orchestrator -run TestWorkerManagerAddAndStartExposesIdleStatus -count=1`

Expected: FAIL because `AddAndStart` and `Statuses` do not exist.

- [ ] **Step 3: Implement the minimum manager API**

Add a `sync.RWMutex`, `logDir`, and `terminal` to `WorkerManager`. Add:

```go
type WorkerStatus struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Cmd string `json:"cmd"`
	Workspace string `json:"workspace"`
	Running bool `json:"running"`
	Busy bool `json:"busy"`
}

func (m *WorkerManager) AddAndStart(cfg CollaboratorConfig) error
func (m *WorkerManager) Statuses() []WorkerStatus
func (m *WorkerManager) Find(id string) *Worker
```

`AddAndStart` rejects duplicate IDs while locked, appends `NewWorker`, then calls `Start`. `Statuses` copies public fields and calls the existing `IsRunning`/ `IsBusy`; it never returns tokens. Replace unsynchronized direct `Workers` iteration in `service.go`, `StartAll`, and `StopAll` with `Find` or a copied slice.

- [ ] **Step 4: Verify GREEN**

Run: `go test ./internal/orchestrator -run 'TestWorker(LifecycleSafety|ManagerAddAndStartExposesIdleStatus)' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/worker.go internal/orchestrator/worker_test.go internal/orchestrator/service.go
git commit -m "feat: add workers at runtime"
```

### Task 3: Register polling immediately

**Files:**
- Modify: `internal/orchestrator/scheduler.go`
- Create: `internal/orchestrator/scheduler_test.go`

- [ ] **Step 1: Write the failing registration test**

```go
func TestSchedulerStartAgentRejectsDuplicate(t *testing.T) {
	s := NewScheduler(nil, time.Hour, nil, nil, "https://gitlab.example.com")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	if err := s.StartAgent(CollaboratorConfig{ID: "coder", GitLabToken: "token"}); err != nil { t.Fatal(err) }
	if err := s.StartAgent(CollaboratorConfig{ID: "coder", GitLabToken: "token"}); err == nil {
		t.Fatal("expected duplicate polling loop error")
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/orchestrator -run TestSchedulerStartAgentRejectsDuplicate -count=1`

Expected: FAIL because `StartAgent` does not exist.

- [ ] **Step 3: Implement runtime polling registration**

Change the constructor to:

```go
func NewScheduler(service *OrchestratorService, interval time.Duration, allowedProjects, allowedAuthors []string, gitlabURL string) *Scheduler
func (s *Scheduler) Start(ctx context.Context)
func (s *Scheduler) StartAgent(col CollaboratorConfig) error
```

Store the root context plus mutex-protected `map[string]context.CancelFunc`. `StartAgent` derives a child context, rejects an existing ID, records its cancellation function, and starts the existing `startPollingForAgent` goroutine. Main calls it once for each stored agent. Keep the current interval and filtering behavior intact.

- [ ] **Step 4: Verify GREEN**

Run: `go test ./internal/orchestrator -run TestSchedulerStartAgentRejectsDuplicate -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/scheduler.go internal/orchestrator/scheduler_test.go
git commit -m "feat: schedule runtime agents"
```

### Task 4: Add the localhost HTTP API and UI

**Files:**
- Create: `internal/orchestrator/web.go`
- Create: `internal/orchestrator/web_test.go`
- Create: `internal/orchestrator/web/index.html`

- [ ] **Step 1: Write failing API tests**

```go
func TestAgentAPIListsStatusesWithoutToken(t *testing.T) {
	h := newTestWebServer(t, []CollaboratorConfig{{ID: "coder", Cmd: "echo", GitLabToken: "secret"}})
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if r.Code != http.StatusOK { t.Fatalf("got %d", r.Code) }
	if strings.Contains(r.Body.String(), "secret") { t.Fatal("token leaked") }
}

func TestAgentAPIAddsAndStartsAgent(t *testing.T) {
	h := newTestWebServer(t, nil)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/agents",
		strings.NewReader(`{"id":"coder","cmd":"echo","workspace":"/tmp","gitlab_token":"secret"}`)))
	if r.Code != http.StatusCreated { t.Fatalf("got %d: %s", r.Code, r.Body.String()) }
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/orchestrator -run TestAgentAPI -count=1`

Expected: FAIL because `NewWebServer` does not exist.

- [ ] **Step 3: Implement the minimal surface**

Embed `web/index.html` and return an `http.NewServeMux` with:

```go
GET  /api/agents
POST /api/agents
GET  /
```

`POST` decodes `CollaboratorConfig`, requires ID/command/workspace/token, rejects duplicate IDs with `409`, and otherwise persists, starts the worker, and starts polling before returning `201`. Use a response DTO with `WorkerStatus` only so tokens can never serialize. Return `400` for malformed/invalid input and `500` for persistence/runtime errors.

Build the HTML with plain form fields, status cards, an inline error area, and:

```js
async function refresh() { /* fetch('/api/agents'), render cards */ }
setInterval(refresh, 2000);
refresh();
```

Do not add a frontend library or WebSocket.

- [ ] **Step 4: Verify GREEN**

Run: `go test ./internal/orchestrator -run TestAgentAPI -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/web.go internal/orchestrator/web_test.go internal/orchestrator/web/index.html
git commit -m "feat: add localhost agent management UI"
```

### Task 5: Wire startup and document the new control surface

**Files:**
- Modify: `cmd/agent-flow/main.go`
- Modify: `internal/orchestrator/config.go`
- Modify: `internal/orchestrator/config_test.go`
- Modify: `README.md`
- Modify: `Makefile`
- Modify: `configs/config.yaml.example`

- [ ] **Step 1: Write the failing config test**

```go
func TestConfigAllowsEmptyLegacyCollaborators(t *testing.T) {
	if err := (&Config{}).Validate(); err != nil {
		t.Fatalf("got %v", err)
	}
}
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/orchestrator -run TestConfigAllowsEmptyLegacyCollaborators -count=1`

Expected: FAIL with `at least one collaborator must be configured`.

- [ ] **Step 3: Wire the store and HTTP server**

In `main.go`, load `data/agents.yaml` using the legacy `cfg.Collaborators`, validate the returned agents, initialize/start workers and scheduler, then call `StartAgent` for each persisted agent. Launch:

```go
go func() {
	if err := http.ListenAndServe("127.0.0.1:8080", orchestrator.NewWebServer(store, workerManager, scheduler)); err != nil {
		slog.Error("Web server stopped", "error", err)
	}
}()
```

Remove only the nonempty collaborator requirement from `Config.Validate`; retain per-agent strict validation when loading/adding runtime agents. Update README to point to `http://127.0.0.1:8080`, explain first-run import and browser management, and make `make status` direct operators to the page instead of hard-coded agent names.

- [ ] **Step 4: Verify all checks**

Run:

```bash
go test ./internal/orchestrator -count=1
go vet ./...
go test ./... -count=1
```

Expected: every command exits 0.

- [ ] **Step 5: Smoke-test localhost**

Run the service with a temporary config/store, then:

```bash
curl --fail http://127.0.0.1:8080/api/agents
curl --fail http://127.0.0.1:8080/ | rg 'Agent'
```

Expected: both exit 0 and the JSON has no token field.

- [ ] **Step 6: Commit**

```bash
git add cmd/agent-flow/main.go internal/orchestrator/config.go internal/orchestrator/config_test.go README.md Makefile configs/config.yaml.example
git commit -m "feat: run agent flow from localhost"
```

