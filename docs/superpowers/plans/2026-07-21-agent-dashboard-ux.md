# Agent Dashboard UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the web console a clearer Agent operations dashboard with status summary, focused modal configuration, and compact Agent cards without changing the API contract.

**Architecture:** Keep the existing embedded single-page HTML and existing settings and agent endpoints. Add UI-only state for centered modals and card detail expansion; use only the current `WorkerStatus` fields, so current/recent MR and scheduler activity remain out of scope.

**Tech Stack:** Embedded HTML, CSS, vanilla browser JavaScript, Go `net/http/httptest`.

---

### Task 1: Lock the embedded dashboard contract with a web handler test

**Files:**

- Modify: `internal/orchestrator/web_test.go`
- Modify: `internal/orchestrator/web/index.html`

- [ ] **Step 1: Write the failing dashboard-markup test**

Add a test that serves `GET /` through `NewWebServer` and requires the HTML to contain the dashboard summary, Agent modal, scheduler modal, and accessible dialog attributes.

```go
func TestWebServerIndexIncludesDashboardControls(t *testing.T) {
    h := NewWebServer(t.TempDir()+"/settings.yaml", NewWorkerManager(nil, t.TempDir(), &MockTerminal{}), nil)
    r := httptest.NewRecorder()
    h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/", nil))
    body := r.Body.String()
    for _, want := range []string{"Agent 總覽", "新增 Agent", "排程設定", "role=\"dialog\""} {
        if !strings.Contains(body, want) { t.Fatalf("missing %q", want) }
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/orchestrator -run TestWebServerIndexIncludesDashboardControls -count=1`

Expected: FAIL because the existing page has no dashboard summary or dialogs.

- [ ] **Step 3: Implement the smallest dashboard shell**

Replace the current page structure with:

``html
<section class="summary" aria-label="Agent 總覽">
  <article class="summary-card"><strong id="idle-count">0</strong><span>待命中</span></article>
  <article class="summary-card"><strong id="busy-count">0</strong><span>執行任務中</span></article>
  <article class="summary-card"><strong id="stopped-count">0</strong><span>已停止</span></article>
</section>
<button type="button" onclick="openModal('agent-modal')">新增 Agent</button>
<button type="button" onclick="openModal('settings-modal')">排程設定</button>
<dialog id="agent-modal" aria-labelledby="agent-modal-title">...</dialog>
<dialog id="settings-modal" aria-labelledby="settings-modal-title">...</dialog>
```

Move the existing forms into their respective dialogs without changing input names, endpoint URLs, validation, or payload construction. Implement `openModal(id)` with `showModal()`, and a close button that calls `close()`.

- [ ] **Step 4: Run the focused test to verify it passes**

Run: `go test ./internal/orchestrator -run TestWebServerIndexIncludesDashboardControls -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the dashboard shell**

```bash
git add internal/orchestrator/web/index.html internal/orchestrator/web_test.go
git commit -m "feat: 重整 Agent 控制台版面"
```

### Task 2: Render compact Agent operations cards

**Files:**

- Modify: `internal/orchestrator/web/index.html`
- Test: `internal/orchestrator/web_test.go`

- [ ] **Step 1: Write the failing card-markup test**

Extend `TestWebServerIndexIncludesDashboardControls` to require `statusClass`, `updateSummary`, and `詳細資料` in the embedded page.

```go
for _, want := range []string{"statusClass", "updateSummary", "詳細資料"} {
    if !strings.Contains(body, want) { t.Fatalf("missing %q", want) }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/orchestrator -run TestWebServerIndexIncludesDashboardControls -count=1`

Expected: FAIL because the new client-side card helpers do not yet exist.

- [ ] **Step 3: Implement compact cards using existing API fields only**

In `refresh()`, compute the three summary counts from `running` and `busy`, then render each card with name, semantic state badge, ID, command, workspace, restart, delete, and a details disclosure.

```js
const updateSummary = agents => {
  document.querySelector('#idle-count').textContent = agents.filter(a => a.running && !a.busy).length;
  document.querySelector('#busy-count').textContent = agents.filter(a => a.busy).length;
  document.querySelector('#stopped-count').textContent = agents.filter(a => !a.running).length;
};
```

Use `<details>` for workspace and command information. Do not render a current MR, recent MR, scheduler event, or log because the existing API does not expose those data.

- [ ] **Step 4: Run the focused test to verify it passes**

Run: `go test ./internal/orchestrator -run TestWebServerIndexIncludesDashboardControls -count=1`

Expected: PASS.

- [ ] **Step 5: Run the full verification**

Run: `env GOCACHE=/tmp/go-build-cache go test ./... -count=1`

Expected: PASS.

- [ ] **Step 6: Verify the Docker-delivered page manually**

Run: `docker compose up --build -d`

Run: `curl --fail --silent http://127.0.0.1:8080/api/agents`

Expected: both Compose services run and the API returns JSON without a 502 response.

- [ ] **Step 7: Commit the compact Agent cards**

```bash
git add internal/orchestrator/web/index.html internal/orchestrator/web_test.go
git commit -m "feat: 強化 Agent 狀態與操作卡片"
```

