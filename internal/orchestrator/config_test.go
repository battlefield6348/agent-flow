package orchestrator

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfig_Validate_StrictMode(t *testing.T) {
	t.Run("當缺少 gitlab_token 時應該報錯", func(t *testing.T) {
		cfg := &Config{
			Collaborators: []CollaboratorConfig{
				{ID: "reviewer", Cmd: "agy", GitLabToken: ""},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("預期會因為缺少 gitlab_token 而報錯，但回傳了 nil")
		}
	})

	t.Run("配置完整時應該通過驗證", func(t *testing.T) {
		cfg := &Config{
			Collaborators: []CollaboratorConfig{
				{ID: "reviewer", Cmd: "agy", GitLabToken: "fake-token"},
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("預期驗證通過，但收到了錯誤: %v", err)
		}
	})
}

func TestConfig_ParsePromptSuffix(t *testing.T) {
	yamlData := `
collaborators:
  - id: "reviewer"
    cmd: "agy"
    gitlab_token: "fake-token"
    prompt_suffix: " 並且在評審完成後標記 @ryan"
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlData), &cfg); err != nil {
		t.Fatalf("Unmarshal 失敗: %v", err)
	}

	if len(cfg.Collaborators) != 1 {
		t.Fatalf("預期有 1 個協作者配置，但得到 %d", len(cfg.Collaborators))
	}

	expected := " 並且在評審完成後標記 @ryan"
	if cfg.Collaborators[0].PromptSuffix != expected {
		t.Errorf("預期 PromptSuffix 為 '%s'，但得到 '%s'", expected, cfg.Collaborators[0].PromptSuffix)
	}
}
