# 實作計畫：通用型本地 AI 協作編排引擎 (Gemini Collaborator Engine)

本計畫旨在實作一個基於 Go 的任務調度平台，透過標籤匹配與 GitLab 整合實現代理人之間的工作流自動化。

## 0. 準備工作 (Phase 0)
- [ ] 初始化 Go 專案與依賴管理。
- [ ] 建立基本的目錄結構 (`/cmd`, `/internal/...`)。
- [ ] 驗證本地 SQLite 與 GitLab API 的連通性。

## 1. 核心基礎與儲存層 (Phase 1)
- [ ] **SQLite 儲存層實作**：
  - 使用 `modernc.org/sqlite`。
  - 實作 `tasks` 表格的 CRUD 操作。
  - 必須包含狀態與標籤查詢邏輯 (ListTasksByTags)。
- [ ] **設定檔處理**：
  - 實作 YAML 讀取模組，解構 `collaborators` 定義。

## 2. 程序編排與 Worker 註冊 (Phase 2)
- [ ] **子程序管理器 (Worker Manager)**：
  - 實作 `os/exec` 的封裝，能啟動 CLI 並監控其存活狀態。
  - 每個 Worker 對應其配置的 `Tags`。
- [ ] **註冊機制**：
  - 實作 Worker 與 Master 之間的握手邏輯（透過 MCP 註冊）。

## 3. MCP 通訊與任務路由 (Phase 3)
- [ ] **MCP Server 實作**：
  - 建立基於 stdio/SSE 的 MCP 門戶。
  - 實作 `poll_available_tasks` 工具：根據 Tags 從 SQLite 獲取 PENDING 任務。
  - 實作 `update_task_status`：處理任務的狀態轉移（State Transition）。
- [ ] **工作流引擎**：
  - 定義 `standard_dev_cycle` 的狀態跳轉字典。

## 4. GitLab 插件整合 (Phase 4)
- [ ] **GitLab Adapter**：
  - 使用 `xanzy/go-gitlab` 封裝 API。
  - 實作 **Pipeline Poller**：監控 `AWAITING_CI` 狀態的任務。
  - 實作 **Comment Scraper**：讀取 MR 討論串並轉化為 Task Payload。
- [ ] **工具擴展**：
  - 新增 `create_mr` 與 `get_comments` 等專業工具供 Coder/Reviewer 使用。

## 5. 整合測試與場景驗證 (Phase 5)
- [ ] **情境測試：開發審核循環**
  - 使用 Mock 或測試 Repo 完整跑完一次 `IDLE -> CODING -> CI -> REVIEW -> APPROVED` 流程。
- [ ] **容錯測試**：
  - 測試 CLI 異常退出時，Orchestrator 是否能自動重啟並恢復狀態。

## 規範提醒 (依據 GEMINI.md)
- 嚴禁在頻繁調用的 API 中使用 `regexp.MustCompile`。
- 所有的任務過濾邏輯必須同時檢查 List 與 Get API 保持一致性。
- 分支推送與 MR 建立時，必須檢查描述是否包含業務價值描述。
