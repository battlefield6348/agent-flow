# agent-flow

`agent-flow` 是一個用 Go 寫成的本地多 AI agent 編排器。它會定時輪詢 GitLab
Todos API，發現待辦 Merge Request 後，透過 `tmux` 啟動對應的本地 AI CLI
worker，在指定 workspace 中執行 review 或開發任務。

## 快速開始

```bash
cp configs/config.yaml.example configs/config.yaml
# 編輯 configs/config.yaml
make check-tools
make start
```

> 前提：系統只透過 GitLab Todos API 得知有工作，而 GitLab 只在「別人對你做動作」時建立
> todo。所以 bot 必須先被加成某個 MR 的 reviewer（或在 MR 留言 `@bot`），否則永遠掃不到
> 任何 MR。詳見 [AGENTS.md](AGENTS.md#觸發機制)。

常用指令：

- `make start`：啟動 orchestrator
- `make stop`：停止 orchestrator 與 tmux session
- `make status`：查看目前 tmux worker 狀態
- `make logs`：追蹤 `logs/` 內輸出
- `make attach-r`：連進 reviewer session
- 其他 worker 沒有對應的 `attach-*`；直接用 `tmux attach -t <collaborator id>` 連入

## 換機 / 新機器設定（從零）

本專案能否重現預期行為，一大半依賴 repo 以外、綁在該機器上的東西。**本節與 `make install-skills`、
skill 入 repo 等設計，都是為了預防「換機/換人就走鐘」的可攜性風險而刻意加入的。** 換到新電腦或
新成員加入時，依序完成下列步驟，缺一項行為就會走鐘：

1. **安裝 CLI 工具**：`go`、`tmux`、`glab`，以及你要用的 AI CLI（`claude` / `codex` / `agy`）。
2. **各自登入認證**：`glab auth`（或靠下方 config 的 token 注入）、`claude` / `codex` / `agy` 的登入。
3. **安裝 skills**：`make install-skills`
   —— 把 repo `skills/` 下的技能 symlink 到各 CLI 對應目錄（claude/codex 用
   `~/.agent-flow/skills/`，agy 用 `~/.gemini/antigravity/skills/`），一行涵蓋三種 CLI。
   留言格式、審查/修正流程都寫在這些技能裡，**不裝就會退化成裸 CLI**。
4. **clone 目標專案**：把要審查/開發的 repo clone 到本機，位置需能被 workspace 掃描解析到。
5. **建立設定檔**：`cp configs/config.yaml.example configs/config.yaml`，填入各 collaborator 的
   token、`workspace` 絕對路徑；若 coder 用 claude，記得把 `--mcp-config` 改成**你本機
   agent-flow 的絕對路徑**（見 example 註解）。
6. **啟動**：`make check-tools && make start`。

> 身分提醒：reviewer 的 token 用 bot 帳號、coder 的 token 用開發者本人。coder 若用 claude，
> `--strict-mcp-config` 會擋掉綁 bot PAT 的 gitlab MCP，確保留言/push 掛在本人名下。

## 文件分工

- [AGENTS.md](AGENTS.md)：所有 agent 共通的專案規則與運作機制
- [configs/config.yaml.example](configs/config.yaml.example)：設定欄位說明與不同 CLI/agent 的建議配置
- [DEVELOPMENT.md](DEVELOPMENT.md)：Go 開發規範與提交前驗證要求
- [CLAUDE.md](CLAUDE.md)：Claude Code 專屬補充
- [GEMINI.md](GEMINI.md)：Antigravity / Gemini (`agy`) 專屬補充

## 這個專案怎麼運作

1. 每個 collaborator 用自己的 `gitlab_token` 輪詢 `pending` 的 MR todos。
2. Todo 經過 MR 狀態、白名單、CI 狀態與 worker 忙碌檢查後，才會派工。
3. Worker 在對應的本地 repo 中執行 CLI 任務。
4. 只有在 worker 產出有效結果後，該 todo 才會被標記為 done。

關於 todo 觸發、技能注入、tmux session、白名單比對與已知限制，請直接看
[AGENTS.md](AGENTS.md)。
