package orchestrator

import (
	"testing"
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
