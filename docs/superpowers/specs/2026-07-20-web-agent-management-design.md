# Web Agent Management Design

## Goal

Run `agent-flow` as a localhost service that displays every agent and lets an operator configure the scheduler and add persisted agents which join the workflow immediately.

## Configuration Boundary

`configs/config.yaml` is read only to start the service. It may contain the listen address, log path, and runtime data-file path. It contains no GitLab settings, polling rules, or agent definitions.

`data/settings.yaml` is the sole persisted workflow configuration. It stores the GitLab URL, poll interval, allowed projects, allowed MR authors, and all agent definitions. The UI is the only configuration surface for these values. A new installation starts with an empty settings file and asks the operator to save scheduler settings before adding an agent.

## Localhost UI

The server binds to the configured local address, defaulting to `127.0.0.1:8080`. The page has a scheduler-settings form, an agent-add form, and status cards. Each card shows ID, name, command, workspace, and `running` / `busy` / `idle`; GitLab tokens never appear in API responses or HTML.

Saving scheduler settings updates the in-memory scheduler and persists the complete settings file. Adding an agent validates and persists it, starts its tmux worker, and starts that agent's GitLab polling loop. Restart restores the same settings, workers, and polling loops.

## Architecture

Use Go's standard `net/http`, embedded HTML/CSS/JavaScript, and the existing YAML dependency. A settings store writes `data/settings.yaml` atomically with owner-only permissions. No database, frontend build system, WebSocket, or authentication is added; the service is local-only and the persisted token file remains within the same local trust boundary as the old configuration.

`WorkerManager` provides synchronized runtime add and status methods. `Scheduler` reads its rules from one synchronized settings snapshot and owns a cancel function for every registered agent loop. The browser polls status every two seconds.

## HTTP Contract

- `GET /api/settings` returns scheduler settings without tokens.
- `PUT /api/settings` validates and persists GitLab URL, interval, and filters, then updates the live scheduler.
- `GET /api/agents` returns agent configuration without tokens plus runtime state.
- `POST /api/agents` validates, persists, starts the worker, and registers polling.
- `GET /` serves the management page.

Invalid input returns `400`; duplicate agent IDs return `409`; store and runtime errors return `500` and are logged.

## Verification

- Unit tests cover startup-config defaults, settings-store round trips, API token omission, duplicate-agent rejection, scheduler updates, and dynamic worker/polling startup.
- Run focused orchestrator tests, `go vet ./...`, `go test ./...`, and a localhost HTTP smoke test.

## Deliberate Limits

- The first version supports setting and adding only; editing, removal, and per-agent pause remain deferred.
- Status uses two-second polling. Add streaming only if the delay proves inadequate.
