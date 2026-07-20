# Host Runner and Web Container Design

## Goal

Run the browser UI in Docker while starting AI CLI agents, tmux sessions, and GitLab polling directly on the host.

## Components

- `agent-flow runner` runs on the host. It owns `data/settings.yaml`, starts local Codex/Claude/Agy commands, and exposes a control API on `127.0.0.1:8081`.
- Docker serves only the existing static Web page on `127.0.0.1:8080`.
- The page calls the host runner through a same-origin reverse proxy in the Web container. Docker maps `host.docker.internal` to the Linux host gateway.

## Behaviour

The runner is the only process that reads or writes workflow settings. It starts with no agents safely. Adding, restarting, and polling agents use the host's installed CLIs and existing authentication; no CLI home directories, project folders, or agent tokens are mounted into Docker.

The Docker service contains only an Nginx configuration and the static HTML. Nginx serves the page and forwards `/api/` to `host.docker.internal:8081`.

## Verification

- Unit tests continue to cover API/worker behavior through the host runner.
- Verify the runner API at port 8081, then start Docker and verify the Web page reaches the proxied API at port 8080.
