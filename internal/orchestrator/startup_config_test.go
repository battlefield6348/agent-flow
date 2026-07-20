package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadStartupConfigDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("logs:\n  path: /tmp/logs\n"), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := LoadStartupConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenAddr != "127.0.0.1:8080" || got.SettingsPath != "data/settings.yaml" {
		t.Fatalf("got %#v", got)
	}
}
