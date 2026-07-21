# Per-Agent Container Design

## Goal

Run every configured agent in its own Docker container. The browser UI remains a
control and monitoring surface only; it never runs an agent or receives Docker
credentials.

This extends the host-runner design: the host `agent-flow` runner owns settings
and calls the local Docker CLI. The Docker-hosted Web page reaches that runner
through the existing reverse proxy.

## Lifecycle

- Adding an agent validates its ID, command, token, and an existing absolute
  workspace path, persists its configuration, then starts
  `agent-flow-<agent-id>`.
- The runner bind-mounts that agent's selected workspace at the same absolute
  path in its container and starts the configured command there.
- Agent status comes from Docker inspection. A stopped, missing, or failed
  container is reported as such instead of being treated as a running worker.
- Deleting an agent stops and removes its named container, cancels its GitLab
  polling loop, and then removes its persisted configuration.
- On runner startup, persisted agents are reconciled with their named
  containers: absent containers are started and existing containers are
  monitored.

## Isolation

Each agent receives only its own secrets:

- Its GitLab token is passed as `GITLAB_TOKEN`, so `glab` in agent A uses A's
  token and cannot read B's token.
- Its Codex and Gemini credentials live in a per-agent Docker volume mounted as
  that container's HOME. Containers never mount the host's `.codex` or
  `.gemini` directories.
- The Web container has no Docker socket and no agent credentials. Only the
  host runner can manage containers.

The persisted settings file remains owner-readable only and API responses omit
every token.

## Minimal Docker Contract

Use one existing agent image for all agents. The runner uses the Docker CLI with
argument arrays, not a shell command, and supplies:

- a deterministic name: `agent-flow-<agent-id>`;
- the selected workspace bind mount;
- a named HOME volume derived from the agent ID;
- `GITLAB_TOKEN` for that agent only; and
- the configured command and arguments.

No dynamic Compose files, Docker SDK, database, or separate token service is
needed.

## HTTP Contract

- `GET /api/agents` returns persisted non-secret agent fields plus Docker
  status.
- `POST /api/agents` creates the persisted agent and its container.
- `DELETE /api/agents/{id}` removes the agent container and its persisted
  configuration. It does not remove the agent HOME volume, so recreating the
  same agent retains its CLI login; explicit credential deletion is deferred.

Invalid configuration returns `400`, duplicate IDs return `409`, and Docker or
store failures return `500` without leaking secrets.

## Verification

- Unit-test the Docker command construction and status mapping with a fake
  command runner.
- Test that A and B receive different token values and HOME volumes.
- Test add, list, restart reconciliation, and delete through the HTTP handler.
- Run focused orchestrator tests followed by `go test ./...`.

## Deliberate Limits

- The first version accepts existing host paths only; it does not clone or
  create workspaces.
- The runner controls only containers named with the `agent-flow-` prefix.
- Resource limits, network policies, and automatic HOME-volume deletion are
  deferred until needed.
