package orchestrator

import "strings"

type CollaboratorConfig struct {
	ID                string   `yaml:"id" json:"id"`
	Name              string   `yaml:"name" json:"name"`
	Cmd               string   `yaml:"cmd" json:"cmd"`
	Args              []string `yaml:"args" json:"args"`
	Skills            []string `yaml:"skills" json:"skills"`
	InputPrefix       string   `yaml:"input_prefix" json:"input_prefix"`
	OnlyFinalResponse bool     `yaml:"only_final_response" json:"only_final_response"`
	Workspace         string   `yaml:"workspace" json:"workspace"`
	GitLabToken       string   `yaml:"gitlab_token" json:"gitlab_token"`
	PromptSuffix      string   `yaml:"prompt_suffix" json:"prompt_suffix"`
}

func DefaultSkills(agentID string) []string {
	switch strings.ToLower(agentID) {
	case "reviewer":
		return []string{"superpowers:git-mr-workflow-reviewer"}
	case "coder":
		return []string{"superpowers:senior-coder-workflow"}
	default:
		return nil
	}
}
