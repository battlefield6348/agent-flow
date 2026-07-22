package orchestrator

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type WorkflowSettings struct {
	GitLabURL        string               `yaml:"gitlab_url" json:"gitlab_url"`
	GitLabToken      string               `yaml:"gitlab_token" json:"gitlab_token"`
	IntervalSeconds  int                  `yaml:"interval_seconds" json:"interval_seconds"`
	CheckCISuccess   bool                 `yaml:"check_ci_success" json:"check_ci_success"`
	AllowedProjects  []string             `yaml:"allowed_projects" json:"allowed_projects"`
	AllowedMRAuthors []string             `yaml:"allowed_mr_authors" json:"allowed_mr_authors"`
	CaoBinPath       string               `yaml:"cao_bin_path" json:"cao_bin_path"`
	CaoSessionName   string               `yaml:"cao_session_name" json:"cao_session_name"`
	CaoServerURL     string               `yaml:"cao_server_url" json:"cao_server_url"`
	Agents           []CollaboratorConfig `yaml:"agents" json:"-"`
}

func LoadWorkflowSettings(path string) (WorkflowSettings, error) {
	candidatePaths := []string{
		path,
		"configs/config.yaml",
		"configs/settings.yaml",
		"data/settings.yaml",
	}

	var targetPath string
	for _, p := range candidatePaths {
		if p != "" {
			if _, err := os.Stat(p); err == nil {
				targetPath = p
				break
			}
		}
	}

	if targetPath == "" {
		targetPath = "configs/config.yaml"
	}

	data, err := os.ReadFile(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return WorkflowSettings{}, nil
	}
	if err != nil {
		return WorkflowSettings{}, err
	}
	slog.Info("已成功載入設定檔", "path", targetPath)
	var settings WorkflowSettings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return WorkflowSettings{}, err
	}
	return settings, nil
}

func SaveWorkflowSettings(path string, settings WorkflowSettings) error {
	data, err := yaml.Marshal(settings)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
