# Web Agent Management Design

## Goal

Run `agent-flow` as a localhost service that shows every agent's state and lets an operator add a persisted agent which joins the worker and scheduling flow immediately.

## Scope

- Serve one browser page from the Go process on localhost.
- Show each agent's ID, name, command, workspace, and `running` / `busy` / `idle` state.
- Accept a new agent's ID, name, command, arguments, skills, workspace, GitLab token, and prompt suffix.
- Persist agents locally so they are restored at the next service start.
- Start the new tmux worker and its GitLab polling loop as part of a successful add request.

The page has no authentication because it binds only to localhost. The GitLab token is stored locally in the same trust boundary as the current YAML configuration.

## Architecture

The scheduler configuration remains in `configs/config.yaml`, but the persisted agent store becomes the sole source of the active agent list. On the first run with no agent-store file, existing `collaborators` from YAML are imported so current deployments retain their agents. Later changes to `collaborators` do not affect the running agent count.

Use the existing YAML dependency to write a small local agent-store file. The store writes atomically through a temporary file and rename, preventing a partial file after interruption. No database or frontend build system is added.

`WorkerManager` gains synchronized runtime add and read operations. `Scheduler` owns a cancel function per polling loop, allowing a newly added agent to begin scanning immediately without restarting the service. The HTTP server reads this runtime state and returns JSON; the browser uses a small polling request every two seconds rather than WebSockets.

## HTTP Contract

`GET /api/agents` returns the persisted agent fields plus runtime state.

`POST /api/agents` accepts the agent fields. It rejects missing required fields and duplicate IDs. On success it persists first, creates and starts the worker, starts the polling loop, and returns the new agent state. If startup fails after persistence, the API returns an error and leaves the stored agent available for the next startup.

`GET /` serves the management page. The page renders agent status cards and an add-agent form; it displays request validation errors inline.

## Runtime Flow

```text
browser POST /api/agents
  -> validate unique agent
  -> atomically persist agent list
  -> WorkerManager.AddAndStart
  -> Scheduler.StartAgent
  -> agent tmux worker and GitLab polling loop active
```

At startup, the process loads the agent store, creates workers for that list, starts them, and starts one polling loop per stored agent. The configured scheduler filters and interval apply to all agents.

## Error Handling and Security

The UI never returns GitLab tokens. API responses omit them. Invalid input, duplicate IDs, persistence failures, and worker startup failures are returned as clear HTTP errors and logged. Binding remains `127.0.0.1`; exposing the service beyond localhost is out of scope because it would require authentication and secure token storage.

## Verification

- Unit tests cover first-run YAML import, atomic store round-trip, duplicate-ID rejection, status serialization without tokens, and a runtime-added agent starting its worker and polling loop.
- Run the focused orchestrator tests, then `go test ./...`, `go vet ./...`, and a local HTTP smoke test against `127.0.0.1`.

## Deliberate Limits

- The first version only adds and observes agents; edit, remove, and per-agent pause controls are deferred until needed.
- Browser updates use two-second polling. Add streaming only if the polling delay becomes a demonstrated problem.
