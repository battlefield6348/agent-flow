# CLAUDE.md

先讀：

- [AGENTS.md](AGENTS.md)
- [DEVELOPMENT.md](DEVELOPMENT.md)

Claude Code 專屬補充只有一點：

- 若 Claude 要作為本專案的自主 worker，`configs/config.yaml` 內對應 collaborator
  的 `args` 應包含 `--dangerously-skip-permissions`

其他專案規則、觸發機制、設定原則與 Go 開發規範，全部以
[AGENTS.md](AGENTS.md)、[configs/config.yaml.example](configs/config.yaml.example)、
[DEVELOPMENT.md](DEVELOPMENT.md) 為準。
