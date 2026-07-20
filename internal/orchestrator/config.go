package orchestrator

import (
	"os"

	"gopkg.in/yaml.v3"
)

type StartupConfig struct {
	Logs struct {
		Path string `yaml:"path"`
	} `yaml:"logs"`
	ListenAddr   string `yaml:"listen_addr"`
	SettingsPath string `yaml:"settings_path"`
}

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

func LoadStartupConfig(path string) (*StartupConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config StartupConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config.Logs.Path == "" {
		config.Logs.Path = "./logs"
	}
	if config.ListenAddr == "" {
		config.ListenAddr = "127.0.0.1:8080"
	}
	if config.SettingsPath == "" {
		config.SettingsPath = "data/settings.yaml"
	}
	return &config, nil
}
