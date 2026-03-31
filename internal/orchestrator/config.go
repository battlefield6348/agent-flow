package orchestrator

import (
"os"

"gopkg.in/yaml.v3"
)

type Config struct {
	Database struct {
		Path string `yaml:"path"`
	} `yaml:"database"`
	Collaborators []CollaboratorConfig `yaml:"collaborators"`
}

type CollaboratorConfig struct {
	ID     string            `yaml:"id"`
	Name   string            `yaml:"name"`
	Cmd    string            `yaml:"cmd"`
	Args   []string          `yaml:"args"`
	Tags   []string          `yaml:"tags"`
	Skills []string          `yaml:"skills"`
	Env    map[string]string `yaml:"env"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}
