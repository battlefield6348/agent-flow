# DEVELOPMENT.md

本檔放 `agent-flow` 的共用開發規範，與使用哪個 AI agent 無關。

跨 agent 的專案運作規範請看 [AGENTS.md](AGENTS.md)。

## 專案架構

- `/cmd`：程式進入點
- `/internal/orchestrator`：主程序邏輯、worker 管理、tmux 適配
- `/internal/gitlab`：GitLab API 對接
- `/configs`：設定檔與範例

## Go 開發規範

### 效能

- 嚴禁在頻繁呼叫的函式內部使用 `regexp.MustCompile`
- 需要重複使用的 regex 必須提升到 package-level 常駐變數

### 一致性

- 修正資料過濾時，必須同步檢查同領域的 `List`、`Get`、`Dropdown` /
  `Options` 相關實作

### 註解與命名

- 禁止無意義的標籤式註解，例如 `// 1. 準備資料`
- 註解應解釋「為什麼這樣做」，不是「現在正在做什麼」
- 命名遵循 Go 慣例（MixedCaps）

### 驗證

提交前至少應在本地執行：

- `go fmt ./...`
- `go vet ./...`
- 相關單元測試

### GitLab MR

- 建立 Merge Request 時，必須補齊業務價值描述
