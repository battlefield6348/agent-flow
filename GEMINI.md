# GEMINI.md - 專案開發規範

## 1. 專案架構
本專案 `gemini-collaborator-go` 是一個以 Go 語言開發的本地任務編排器 (Orchestrator)。

### 目錄結構
- `/cmd`: 程式進入點
- `/internal/orchestrator`: 主程序邏輯與程序管理
- `/internal/mcp`: MCP Server 實作與工具定義
- `/internal/repository`: SQLite 資料存取層
- `/internal/gitlab`: GitLab API 整合邏輯
- `/configs`: 範例與預設設定檔

## 2. Go 開發禁忌與最佳實踐
所有貢獻者必須遵循以下規則：

### [效能]
- **嚴禁**在頻繁呼叫的函式內部使用 `regexp.MustCompile`。必須將其提取為 package 層級的常駐變數。

### [一致性]
- 修正資過濾（如任務狀態篩選、專案權限）時，必須同時檢查同領域的所有 List、Get 與 Dropdown/Options 相關實作，確保過濾邏輯同步。

### [規範]
- **嚴格禁止標籤式註解**：如 `// 1. 準備數據`、`// 2. 執行任務` 等這類無意義的廢話註解。
- 註解應解釋「為什麼這樣做」而非「正在做什麼」。
- **命名規範**：遵循 Go 標準命名慣例（MixedCaps）。

### [驗證]
- 提交任何變更前，必須在本地環境執行過 `go fmt`、`go vet` 以及相關的單元測試。

### [GitLab MR]
- 建立 Merge Request 時，必須補齊**業務價值描述**，不可留空。

## 3. 技術選型
- **DB**: SQLite (使用 `modernc.org/sqlite` 之類的純 Go 驅動)。
- **GitLab SDK**: `github.com/xanzy/go-gitlab`。
- **MCP Framework**: 遵循 MCP 官方規格實作 stdio 溝通媒介。
