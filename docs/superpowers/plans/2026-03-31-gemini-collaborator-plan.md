# 實作計畫：本地 AI 協作編排器 (agent-flow)

本計畫旨在實作一個基於 Go 的定時排程編排器，透過監聽 GitLab 的 Merge Requests 標記，自動協調本地 tmux 中的 AI 實體 (Workers) 進行程式碼評審。

## 0. 準備工作 (Phase 0)
- [x] 初始化 Go 專案與依賴管理。
- [x] 建立基本目錄結構 (`cmd/agent-flow`, `internal/orchestrator`)。
- [x] 驗證 GitLab API 的連通性。

## 1. 核心配置與 Worker 管理 (Phase 1)
- [x] **設定檔處理**：
  - 實作 YAML 設定檔讀取，支援 `logs`、`scheduler` 與 `collaborators` 配置項目。
- [x] **tmux 子程序管理器 (Worker Manager)**：
  - 實作基於 `tmux` 命令的 Worker 封裝，支援啟動/停止 session。
  - 實作 `SendInput` 與畫面輪詢（透過 `tmux capture-pane`）以抓取 AI 的回答。
  - 實作回答寫入 `${sessionID}_answer.txt` 檔案的通訊機制。

## 2. GitLab 排程監聽與 Workspace 切換 (Phase 2)
- [x] **GitLab Scheduler Poller**：
  - 實作背景 Ticker 定期執行掃描（依據 `scheduler.interval_seconds`）。
  - 使用 GitLab API 拉取開放中的 MRs，偵測 `#reviewer` 標記或指派的 Reviewer 名單。
  - 實作 `processed_mrs` 去重映射表，僅在首次發現或 SHA 更新時觸發。
- [x] **工作區切換與任務派發**：
  - 解析 MR Web URL 取得專案路徑，並比對本地專案目錄的 Git remote URL 以定位本地工作區。
  - 動態切換 Reviewer Worker 的執行路徑 (Cwd)，重啟該 Worker 會話。
  - 將 review 指令發送至 Worker 開始評審。

## 3. 整合測試與場景驗證 (Phase 3)
- [ ] **情境測試：排程評審閉環**
  - 使用測試 GitLab 專案提交含有 `#reviewer` 標記的 MR。
  - 觀察背景 Scheduler 是否能自動偵測、定位本地工作區並切換 Reviewer Worker。
  - 驗證 Reviewer 是否產出評審回答並寫入 `logs/reviewer_answer.txt`。

## 規範提醒 (依據 GEMINI.md)
- 嚴禁在頻繁調用的函式中使用 `regexp.MustCompile`。
- 註解說明應著重於「為什麼這樣做」而非「正在做什麼」。
- 建立 Merge Request 時，必須補齊業務價值描述。
