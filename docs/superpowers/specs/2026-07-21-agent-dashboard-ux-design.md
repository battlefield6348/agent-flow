# Agent Dashboard UX Design

## Goal

Make the web console useful for both continuous Agent monitoring and frequent scheduler or Agent configuration, without changing the existing API contract.

## Layout

The dashboard remains a single operational page. A compact summary row shows counts for idle, busy, and stopped/attention-needed Agents. Agent cards are the primary content and show the role, semantic status color, current or most recent Merge Request, a short task summary, and high-frequency actions.

This iteration does not add scheduler logs, activity history, or a new runtime-status API. Operators continue to use `docker compose logs -f agent-flow` for detailed scheduler output.

## Interactions

- Add Agent opens a centered modal.
- Scheduler settings open a centered modal.
- Agent edit/details open a centered modal or details view from the card.
- Delete remains a confirmation dialog.
- Restart is available directly on the Agent card.

The modal form keeps the current fields and validation. It does not add any new configuration semantics.

## Status and Feedback

- Idle: green.
- Busy: purple.
- Stopped: gray.
- Success, information, warning, and error feedback use green, blue, amber, and red.

## Data Boundary

The existing agent response exposes only ID, name, command, workspace, running, and busy. Current/recent MR and activity display are therefore out of scope until an additive runtime-status response exists. The initial UI change must not invent or display task details that the API does not provide.

## Verification

- Existing web handler tests continue to pass.
- Add browser-facing tests for modal open/close behavior and rendering the three Agent states if the project introduces a browser test harness; otherwise verify the rendered HTML and API integration manually with Docker Compose.
- Confirm the dashboard remains usable at the current mobile breakpoint.
