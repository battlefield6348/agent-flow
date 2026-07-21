# PR-1 coder 功能整合設計

## 目標

將 `pr-1` 中尚未存在的 coder 派工與完成驗證整合進目前的多 agent 架構，不回退網頁管理、動態排程或現有設定格式。

## 範圍

- `coder` 僅在 MR 留言含有審查結論且標示「需修改後再審」時接收任務；其他 coder Todo 直接結案，避免非預期自動修改與無限輪詢。
- Worker 成功後，服務讀取該 agent 新增的最新 MR 留言；只有符合角色格式才結案。coder 需要 `## 修正回覆`，reviewer 接受 `## 審查結論` 或相容的舊格式。
- 擴充既有 GitLab repository adapter 讀取 MR notes，使用既有 per-agent token 取得對應 bot 身分。
- 保留目前的 scheduler、worker manager、網頁 API、設定與文件；不移植 `pr-1` 的舊版設定、README、Makefile 或 skills。

## 資料流

`Scheduler` 仍為每個 agent 建立輪詢。`OrchestratorService` 在指派前對 coder 查詢 MR notes；通過後將 Todo、MR 與派工前最新 bot note ID 綁到既有 `WorkerTask.OnSuccess`。成功回呼只接受該 bot 的新且格式正確的留言，再呼叫既有 Todo 結案 API。

## 錯誤處理

讀取 notes、取得 bot 使用者或驗證完成留言失敗時不結案，讓 Todo 留待下一輪重試。非 opened MR 與未要求修改的 coder Todo 維持既有清理行為。

## 驗證

新增服務層測試：coder gate、有效完成留言結案、無新留言或格式不符時不結案；執行 `go test ./... -count=1`。
