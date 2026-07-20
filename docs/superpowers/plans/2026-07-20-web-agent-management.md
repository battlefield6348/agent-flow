# Web Agent Management Implementation Plan

Goal: configure all workflow behaviour from localhost Web UI; startup YAML only controls the service address, logs, and settings-file path.

Architecture: data/settings.yaml atomically persists GitLab polling fields and agents. Existing Go workers and scheduler receive minimal synchronized runtime add/update operations. A standard-library HTTP server embeds a single polling page.

## Task 1: Startup-only configuration

Files: internal/orchestrator/config.go, config_test.go, configs/config.yaml.example

- [ ] Write TestLoadStartupConfigDefaults for a YAML file with only logs.path and assert 127.0.0.1:8080 plus data/settings.yaml defaults.
- [ ] Run env GOCACHE=/tmp/go-build-cache go test ./internal/orchestrator -run TestLoadStartupConfigDefaults -count=1 and verify RED.
- [ ] Replace Config with StartupConfig containing Logs.Path, ListenAddr, and SettingsPath. Remove scheduler, GitLab, and collaborator fields. Add LoadStartupConfig with defaults.
- [ ] Re-run the focused test and verify GREEN.

## Task 2: Persist Web-managed workflow settings

Files: create internal/orchestrator/settings_store.go and settings_store_test.go

- [ ] Write TestSettingsStoreRoundTrip with WorkflowSettings containing GitLabURL, IntervalSeconds, filters, and an agent with a token.
- [ ] Run env GOCACHE=/tmp/go-build-cache go test ./internal/orchestrator -run TestSettingsStoreRoundTrip -count=1 and verify RED.
- [ ] Define WorkflowSettings and load/save functions. Missing settings files return an empty value. Save with MkdirAll, CreateTemp, chmod 0600, close, and Rename.
- [ ] Re-run the focused test and verify GREEN.

## Task 3: Add workers and polling at runtime

Files: internal/orchestrator/worker.go, service.go, scheduler.go, worker_test.go; create scheduler_test.go

- [ ] Write TestWorkerManagerAddAndStartExposesStatus: add one echo worker, assert an ID and running status, then assert duplicate ID rejection.
- [ ] Run its focused test and verify RED.
- [ ] Add a mutex, retained terminal/log path, AddAndStart, Find, Statuses, and token-free WorkerStatus. Replace unsynchronized worker iteration in service.go.
- [ ] Add Scheduler.Update(settings), Start(ctx), and StartAgent(agent). Scheduler keeps a root context and cancellation map, starts each agent loop once, and reads current GitLab/interval/filter settings.
- [ ] Run focused worker and scheduler tests and verify GREEN.

## Task 4: Localhost settings and agent API

Files: create internal/orchestrator/web.go, web_test.go, web/index.html

- [ ] Write TestSettingsAPIHidesAgentTokens and TestAgentAPIAddsRunningAgent using httptest.
- [ ] Run env GOCACHE=/tmp/go-build-cache go test ./internal/orchestrator -run 'TestSettingsAPI|TestAgentAPI' -count=1 and verify RED.
- [ ] Embed the HTML and register GET/PUT /api/settings, GET/POST /api/agents, and GET /. Settings updates validate and persist then update Scheduler. Agent adds validate, persist, start a worker, and start polling; API DTOs never include a token.
- [ ] Add vanilla HTML/CSS/JS for settings form, add-agent form, status cards, inline errors, and two-second fetch polling.
- [ ] Re-run the API tests and verify GREEN.

## Task 5: Wire startup and verify

Files: cmd/agent-flow/main.go, README.md, Makefile

- [ ] Load only StartupConfig and WorkflowSettings. Start persisted workers and scheduler loops; serve the Web handler on StartupConfig.ListenAddr. Remove config collaborator assumptions and fixed first-agent token use.
- [ ] Update docs: startup config boundary, Web workflow configuration, localhost URL, and Web-first status command.
- [ ] Run env GOCACHE=/tmp/go-build-cache go test ./internal/orchestrator -count=1, go vet ./..., and go test ./... -count=1.
- [ ] Run a temporary localhost service and curl /api/settings plus /; verify no token appears.

