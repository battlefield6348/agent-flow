# PR-1 coder 功能整合 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 將 coder 派工守門與 GitLab 留言完成驗證併入目前的多 agent 管理架構。

**Architecture:** 延伸既有 GitLab client/adapter 的 MR notes 讀取能力。OrchestratorService 在派工前檢查 coder 是否真的有待修審查，並在 Worker 成功後驗證對應 bot 的新 MR 留言再結案；scheduler、web API 與設定格式維持不變。

**Tech Stack:** Go、標準庫 net/http、現有 GitLab REST v4 adapter、Go testing。

---

### Task 1: 讀取 MR notes 的 GitLab 邊界

**Files:**

- Modify: `internal/gitlab/client.go`
- Modify: `internal/orchestrator/domain.go`
- Modify: `internal/orchestrator/gitlab_adapter.go`
- Modify: `internal/orchestrator/service.go`
- Test: `internal/orchestrator/gitlab_test.go`

- [ ] **Step 1: 寫 adapter 的失敗測試**

在 `gitlab_test.go` 加入測試 server，要求 `GET /api/v4/projects/group%2Frepo/merge_requests/7/notes`，回傳兩筆 `{id, body, author:{username}}`；呼叫尚不存在的 `FetchMergeRequestNotes`。

```go
notes, err := repo.FetchMergeRequestNotes(context.Background(), "group/repo", 7)
if err != nil || !reflect.DeepEqual(notes, []Note{{ID: 1, Body: "## 審查結論", Author: "reviewer"}}) {
    t.Fatalf("notes=%+v err=%v", notes, err)
}
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/orchestrator -run TestHttpGitLabRepository_FetchMergeRequestNotes -count=1`

Expected: FAIL，因 `FetchMergeRequestNotes` 與 `Note` 尚未定義。

- [ ] **Step 3: 實作最小 notes 讀取路徑**

在 client 加入 DTO 與方法；沿用 pipelines 的 URL escaping、`PRIVATE-TOKEN` header 和非 200 錯誤處理。

```go
type NoteDTO struct {
    ID     int    `json:"id"`
    Body   string `json:"body"`
    Author struct{ Username string `json:"username"` } `json:"author"`
}

func (c *Client) FetchMergeRequestNotes(ctx context.Context, projectPath string, mrIID int) ([]NoteDTO, error) {
    apiURL := fmt.Sprintf("%s/api/v4/projects/%s/merge_requests/%d/notes", c.baseURL, url.PathEscape(projectPath), mrIID)
    // 與 FetchMergeRequestPipelines 相同地 GET、解碼 []NoteDTO 並回傳。
}
```

在 domain 定義 `Note{ID, Body, Author}`，在 `GitLabRepository` 與 `HttpGitLabRepository` 增加 `FetchMergeRequestNotes`，逐筆 DTO 對應成 domain `Note`。

- [ ] **Step 4: 執行 adapter 測試確認通過**

Run: `go test ./internal/orchestrator -run TestHttpGitLabRepository_FetchMergeRequestNotes -count=1`

Expected: PASS。

- [ ] **Step 5: 提交 GitLab notes 邊界**

```bash
git add internal/gitlab/client.go internal/orchestrator/domain.go internal/orchestrator/gitlab_adapter.go internal/orchestrator/service.go internal/orchestrator/gitlab_test.go
git commit -m "feat: 讀取 Merge Request 留言"
```

### Task 2: 將 worker 派工成功回呼保留到完成時刻

**Files:**

- Modify: `internal/orchestrator/worker.go`
- Test: `internal/orchestrator/worker_test.go`

- [ ] **Step 1: 寫成功回呼測試**

使用既有 fake terminal 建立 worker，驗證 `SendTask(WorkerTask{Text: "任務", OnSuccess: ...})` 會將 `Text` 送進既有 input 流程，且 `handleInput` 在 `pollUntilReady` 成功後才呼叫一次回呼。

```go
called := make(chan struct{}, 1)
w.SendTask(WorkerTask{Text: "任務", OnSuccess: func(string) { called <- struct{}{} }})
// 驅動既有 ready fake terminal 後：
select { case <-called: default: t.Fatal("OnSuccess was not called") }
```

- [ ] **Step 2: 執行測試確認失敗**

Run: `go test ./internal/orchestrator -run TestWorkerTaskOnSuccess -count=1`

Expected: FAIL，因 `WorkerTask` 與 `SendTask` 尚未存在。

- [ ] **Step 3: 以最小型別取代字串 queue**

```go
type WorkerTask struct {
    Text string
    OnSuccess func(string)
}

func (w *Worker) SendInput(text string) { w.SendTask(WorkerTask{Text: text}) }
func (w *Worker) SendTask(task WorkerTask) {
    if w.Config.InputPrefix != "" { task.Text = w.Config.InputPrefix + task.Text }
    w.inputCh <- task
}
```

將 `inputCh` 改為 `chan WorkerTask`，讓 `handleInput` 接收 task；在既有 `processAndSaveOutput` 後，僅當 output 非空且任務回呼非 nil 時呼叫 `task.OnSuccess(output)`。一般 `SendInput` 不附回呼，行為不變。

- [ ] **Step 4: 執行 worker 測試確認通過**

Run: `go test ./internal/orchestrator -run 'TestWorker(TaskOnSuccess|LifecycleSafety|BuildPromptMsg)' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交 worker 回呼**

```bash
git add internal/orchestrator/worker.go internal/orchestrator/worker_test.go
git commit -m "feat: 任務完成後驗證 GitLab 留言"
```

### Task 3: coder 守門與 Todo 完成驗證

**Files:**

- Modify: `internal/orchestrator/service.go`
- Test: `internal/orchestrator/service_test.go`

- [ ] **Step 1: 擴充 mock 並寫失敗測試**

擴充 `MockGitLabRepository` 的 `Notes`、`Username`、`MarkedTodoIDs` 欄位及 `FetchMergeRequestNotes`、`GetUsername`、`MarkTodoAsDone` 實作。新增三個 `t.Run`：

```go
t.Run("coder skips todo without requested changes", func(t *testing.T) {
    repo.Notes = []Note{{Body: "## 審查結論\n核准"}}
    requireMarkedTodo(t, repo, todo.ID)
    requireNoWorkerTask(t, worker)
})
t.Run("coder assigns requested changes", func(t *testing.T) {
    repo.Notes = []Note{{Body: "## 審查結論\n需修改後再審"}}
    requireWorkerTask(t, worker, "請處理 Merge Request")
})
t.Run("completion requires a new role-valid bot note", func(t *testing.T) {
    repo.Notes = append(repo.Notes, Note{ID: previousID + 1, Author: repo.Username, Body: "## 修正回覆"})
    requireMarkedTodo(t, repo, todo.ID)
})
```

- [ ] **Step 2: 執行服務測試確認失敗**

Run: `go test ./internal/orchestrator -run 'TestOrchestratorService_(Coder|Completion)' -count=1`

Expected: FAIL，因目前一派工就直接結案，且沒有 notes gate。

- [ ] **Step 3: 實作角色規則與完成驗證**

在 `service.go` 集中角色常數與 helper；不新增設定欄位。

```go
const ( agentIDReviewer = "reviewer"; agentIDCoder = "coder" )

func (s *OrchestratorService) mrNeedsCoderFix(ctx context.Context, repo GitLabRepository, project string, iid int) (bool, error) {
    notes, err := repo.FetchMergeRequestNotes(ctx, project, iid)
    if err != nil { return false, err }
    for _, note := range notes {
        body := strings.TrimSpace(note.Body)
        if (strings.HasPrefix(body, "## 審查結論") || strings.HasPrefix(body, "### 結論")) && strings.Contains(body, "需修改後再審") { return true, nil }
    }
    return false, nil
}
```

在 `ScanAndAssignForAgent` 的 busy check 後執行 coder gate；不符合時 `MarkTodoAsDone` 後 continue。派工前以 agent token 的 `GetUsername` 和 notes 記錄最新 bot note ID；把 closure 傳給 `assignToWorker`。closure 僅接受 ID 大於派工前記錄、作者相同且格式有效的新留言：coder 需 `## 修正回覆`，reviewer 接受 `## 審查結論` 或 `### 結論`；成功時才 `MarkTodoAsDone`。取得/驗證留言失敗時只記錄錯誤，保留 Todo。

將 `assignToWorker` 改為接受 `onSuccess func(string)` 並呼叫 `w.SendTask`；coder instruction 明確要求讀取最新審查結論、修正同一來源分支、發出 `## 修正回覆`。

- [ ] **Step 4: 執行服務測試確認通過**

Run: `go test ./internal/orchestrator -run 'TestOrchestratorService_(Coder|Completion|ScanAndAssign)' -count=1`

Expected: PASS。

- [ ] **Step 5: 提交服務邏輯**

```bash
git add internal/orchestrator/service.go internal/orchestrator/service_test.go
git commit -m "feat: 僅結案已驗證的 coder 任務"
```

### Task 4: 全量驗證與計畫收尾

**Files:**

- Modify: `docs/superpowers/plans/2026-07-21-pr-1-coder-integration.md`

- [ ] **Step 1: 格式化變更**

Run: `gofmt -w internal/gitlab/client.go internal/orchestrator/domain.go internal/orchestrator/gitlab_adapter.go internal/orchestrator/service.go internal/orchestrator/worker.go internal/orchestrator/*_test.go`

Expected: exit 0。

- [ ] **Step 2: 執行完整測試**

Run: `GOCACHE=/tmp/go-build-cache go test ./... -count=1`

Expected: PASS。

- [ ] **Step 3: 檢查差異與提交計畫追蹤更新**

Run: `git diff --check && git status --short`

Expected: 無空白錯誤；僅有計畫核取方塊的預期更新。

```bash
git add docs/superpowers/plans/2026-07-21-pr-1-coder-integration.md
git commit -m "docs: 完成 coder 整合計畫"
```
