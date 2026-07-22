package orchestrator

// CollaboratorConfig 定義個案 Agent 輪詢設定
type CollaboratorConfig struct {
	ID           string `yaml:"id" json:"id"`
	GitLabToken  string `yaml:"gitlab_token" json:"gitlab_token"`
	PromptSuffix string `yaml:"prompt_suffix" json:"prompt_suffix"`
}
