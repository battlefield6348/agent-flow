# GEMINI.md - 專案開發規範

## 1. 專案架構
本專案 `gemini-collaborator-go` 是一個以 Go 語言開發的本地任務編排器 (Orchestrator)。

### 目錄結構
- `/cmd`: 程式進入點
- `/internal/orchestrator`: 主程序邏輯與程序管理
- `/configs`: 範例與預設設定檔

## 2. Go 開發禁忌與最佳實踐
所有貢獻者必須遵循以下規則：

### [效能]
- **嚴禁**在頻繁呼叫的函式內部使用 `regexp.MustCompile`。必須將其提取為 package 層級的常駐變數。

### [一致性]
- 修正資料過濾時，必須同時檢查同領域的所有 List、Get 與 Dropdown/Options 相關實作，確保過濾邏輯同步。

### [規範]
- **嚴格禁止標籤式註解**：如 `// 1. 準備數據` 等這類無意義的廢話註解。
- 註解應解釋「為什麼這樣做」而非「正在做什麼」。
- **命名規範**：遵循 Go 標準命名慣例（MixedCaps）。

### [驗證]
- 提交任何變更前，必須在本地環境執行過 `go fmt`、`go vet` 以及相關的單元測試。

### [GitLab MR]
- 建立 Merge Request 時，必須補齊**業務價值描述**，不可留空。

## 3. 技術選型
- **GitLab API**: 直接使用 Go 原生 HTTP Client 連接 GitLab 待辦事項 (Todos) API 進行狀態追蹤。
- **Worker 管理**: 透過本地 `tmux` 同步管理與通訊。
