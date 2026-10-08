# Agent Flow：GitLab Todo 驅動的本地 AI 多代理編排器 (CAO 整合版)

[前置需求與 CAO 安裝](#前置需求與-cao-安裝) · [CAO Profiles 與 Skills 設定](#-cao-profiles-與-skills-設定指南) · [配置說明](#配置說明) · [快速啟動](#快速啟動) · [系統架構](#系統架構)

Agent Flow 是一個以 Go 語言開發的輕量級本地多代理編排器。它會輪詢 GitLab Merge Request 的 Todos，將工作直接派發給 AWS Labs `cli-agent-orchestrator` (CAO) 的 Supervisor 終端機進程，並以 GitLab 留言驗證任務結果。

---

## 🔗 前置需求與 CAO 安裝

本專案高度整合 AWS Labs 開源的 CLI Agent 編排工具 **[cli-agent-orchestrator (CAO)](https://github.com/awslabs/cli-agent-orchestrator)**。

### 1. 安裝 CLI Agent Orchestrator (CAO)

確保你的 Python 環境已就緒，隨後透過 `pip` 或 `pipx` 安裝 CAO：

```bash
# 使用 pipx (推薦)
pipx install cli-agent-orchestrator

# 或使用 pip
pip install cli-agent-orchestrator
```

驗證安裝：

```bash
cao --version
```

### 2. 啟動 CAO 背景服務

在啟動 Agent Flow 前，請確保 `cao-server` 已在背景運行：

```bash
cao-server &
```

---

## 🧩 CAO Profiles 與 Skills 設定指南

在 CAO 體系中，**Profile** 定義了 Agent 的角色與權限，而 **Skills** 為 Agent 提供了擴充的工具與工作流。

### 1. Agent Profiles 管理 (`cao profile`)

查看目前系統中已建立的 Agent Profiles：

```bash
cao profile list
```

建立或修改專屬 Agent Profile (例如 `review_supervisor` 或 `code_supervisor`)：

```bash
# 檢視特定 Profile 設定
cao profile show review_supervisor

# 建立自訂 Agent Profile (指定角色與工具權限)
cao profile create review_supervisor \
  --role "Reviewer Supervisor" \
  --allowed-tools "@builtin,fs_*,execute_bash"
```

建立完成後，即可在 `configs/config.yaml` 的 `cao_agent_profile` 欄位填入該 Profile 名稱。

### 2. Skills 技能管理 (`cao skills`)

CAO 支援透過 Skills 擴充 Agent 的自動化能力 (例如帶入特定的 Code Review 規範與專案範本)。

查看目前已安裝的 Skills 清單：

```bash
cao skills list
```

安裝新的 Skill (從本地目錄、Git URL 或遠端商店)：

```bash
# 從本地目錄安裝專案技能
cao skills install ./path/to/my-skill

# 從遠端 Git 儲存庫安裝技能
cao skills install https://github.com/user/my-agent-skill
```

---

## 🏗️ 系統架構

```mermaid
flowchart LR
  GitLab["GitLab Todos API"] --> AgentFlow["Agent Flow Daemon (Go)"]
  AgentFlow --> CAODispatcher["CAO Task Dispatcher"]
  CAODispatcher --> CAOServer["cao-server REST / CLI"]
  CAOServer --> Supervisor["CAO Supervisor Session (tmux)"]
```

---

## ⚙️ 配置說明

本專案採用 **完全配置驅動 (Config-Driven Architecture)**，無需修改任何程式碼或寫死 Bash 腳本即可自由定義 Agents、Session 名稱、Profile 與 AI Provider。

### 1. 複製設定檔範本

```bash
cp configs/config.yaml.example configs/config.yaml
```

### 2. 編輯 `configs/config.yaml`

```yaml
gitlab_url: "https://gitlab.your-company.com"
interval_seconds: 60
check_ci_success: true

# cao-server 連線位址
cao_server_url: "http://localhost:9889"

allowed_projects:
  - "group/project-a"
allowed_mr_authors:
  - "developer1"

# 自由定義 Agent 角色、專屬 Token、目標 Session 名稱、Agent Profile 與 Provider
agents:
  - id: "reviewer"
    role: "reviewer"                            # 角色：reviewer 或 coder (未填時沿用 id)
    replicas: 3                                 # Session 數量，MR 會分流給閒置的 Session (建立 gitlab-reviewer-1 ~ 3)
    gitlab_token: "glpat-reviewer-token-xxxxxx"
    cao_session_name: "gitlab-reviewer"         # 自訂 Session 名稱
    cao_agent_profile: "review_supervisor"      # 自訂 CAO Agent Profile
    cao_provider: "antigravity_cli"             # 自訂 Provider (相容 agy, kiro_cli, codex 等)

  - id: "coder"
    role: "coder"                               # coder 共用本機 Repo，固定只會建立 1 個 Session
    gitlab_token: "glpat-coder-token-yyyyyy"
    cao_session_name: "gitlab-coder"            # 自訂 Session 名稱
    cao_agent_profile: "code_supervisor"        # 自訂 CAO Agent Profile
    cao_provider: "codex"                       # 自訂 Provider
```

### 3. Reviewer 分流 (Session Pool)

同一個 Agent 設定 `replicas: N` 後，會由單一輪詢迴圈抓取該帳號的 Todos，再分派給 `<cao_session_name>-1` ~ `-N` 中閒置的 Session：

- 全部 Session 都忙碌時，剩餘 Todo 會保留在 GitLab，下一輪輪詢再處理。
- 同一張 MR 仍在某個 Session 處理中時，新的 Todo 會延後，不會同時派給另一個 Session。
- `replicas` 未設定或為 1 時沿用原本的 Session 名稱，既有部署不受影響。
- 每個 Session 都是一個獨立的 AI CLI 進程，請依 Provider 的 Rate Limit 與費用調整數量，建議從 2 ~ 3 開始。

---

## 🚀 快速啟動

### 一鍵啟動 (One-Command Start)

```bash
make start
```

`make start` 會自動檢測 `cao-server` 狀態、依據 `config.yaml` 自動動態建立與對接相應的 CAO Sessions，並啟動背景輪詢服務。

### 一鍵優雅關閉 (Graceful Shutdown)

在終端機按下 `Ctrl+C` 或執行以下指令：

```bash
make stop
```

系統會自動停止輪詢，並同步深層清理背景所有 CAO/tmux Sessions 與資料庫殘留紀錄。

### Session 自動恢復

每次輪詢前會確認各 Agent 的 CAO Session 是否運作中；若派發時發現 Session 已不存在，會自動重新啟動並重送任務，無需手動 `cao launch`。

> ⚠️ `gitlab_token` 會在 Session 啟動時注入環境變數。修改 `config.yaml` 中的 Token 後，請執行 `make stop` 再 `make start` 使其生效。

### 以 Docker 啟動

映像內已安裝 `agent-flow`、CAO (`cao` / `cao-server`)、`tmux`、`glab`、Claude Code 與 Codex CLI；容器啟動時會自動帶起容器內的 `cao-server`，並依 `config.yaml` 建立各 Agent 的 CAO Sessions。

```bash
make docker-up     # 建置映像並於背景啟動
make docker-logs   # 追蹤日誌
make docker-down   # 關閉容器並清理容器內所有 CAO Sessions
```

容器以與主機相同的 UID/GID 與家目錄路徑執行，並掛載以下主機目錄，因此主機上需先完成各項登入：

| 主機路徑 | 用途 |
| :--- | :--- |
| `~/projects` (可用 `AGENT_FLOW_PROJECTS_DIR` 覆寫) | 本專案與 MR 對應的本地 Repo，`configs/config.yaml` 與 `logs/` 也由此讀寫 |
| `~/.aws/cli-agent-orchestrator/agent-store`、`skills` | CAO Agent Profiles 與 Skills |
| `~/.claude`、`~/.claude.json`、`~/.codex` | Claude Code / Codex 登入憑證 |
| `~/.local/bin/agy` (可用 `AGY_BIN` 覆寫)、`~/.gemini` | Antigravity CLI (`cao_provider: agy`) 執行檔與憑證 |
| `~/.gitconfig`、`~/.git-credentials`、`~/.ssh`、`~/.config/glab-cli` | Git 推送與 `glab` 憑證 |

> ⚠️ 請勿同時執行 `make start` 與 Docker 版本：兩者會重複輪詢同一批 GitLab Todos，並共用 `logs/` 下的資料庫。
>
> 💡 CAO 目前的 Google 系 Provider 為 Antigravity CLI (`agy`)，並未支援 Gemini CLI，因此映像中不安裝 `gemini`。

---

## 🛠️ 開發與常用指令

| 指令 | 說明 |
| :--- | :--- |
| **`make start`** | 自動診斷環境、自動建立 CAO Sessions 並啟動背景輪詢 |
| **`make stop`** | 優雅關閉輪詢服務並深層清理所有 CAO/tmux Sessions |
| **`make cao-status`** | 查看目前電腦中運作中的 CAO Sessions 狀態 |
| **`make test`** | 執行全套 Golang 單元測試 |
| **`make fmt`** | 執行程式碼格式化與靜態檢查 (`go fmt` + `go vet`) |
| **`make build`** | 編譯二進位執行檔 `agent-flow` |
| **`make docker-up`** | 以 Docker 建置並於背景啟動 Agent Flow |
| **`make docker-down`** | 關閉 Docker 容器並清理容器內 CAO Sessions |
| **`make docker-logs`** | 追蹤 Docker 容器日誌 |

---

## 📜 授權

請參閱 [LICENSE](LICENSE)。
