package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOsWorkspaceRepository_FindLocalPath(t *testing.T) {
	// 建立臨時目錄模擬 projects 目錄
	tempDir, err := os.MkdirTemp("", "agent-flow-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// 建立一個模擬的專案目錄
	projDir := filepath.Join(tempDir, "my-project")
	_ = os.MkdirAll(filepath.Join(projDir, ".git"), 0755)

	// 切換工作目錄到 tempDir 的子目錄，模擬實際執行環境
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	_ = os.Chdir(projDir)

	// 這裡我們難以在測試環境中真正執行 git remote get-url (因為沒有真的 git repo)
	// 但我們可以先測試基本邏輯。由於目前的實作依賴 exec.Command("git")，
	// 建議在實作中加入可注入的 Runner 或是針對此測試進行環境適配。
	
	// 目前先撰寫一個會失敗的測試（因為還沒實作程式碼）
	repo := NewOsWorkspaceRepository()
	_, err = repo.FindLocalPath(context.Background(), "namespace/my-project")
	if err == nil {
		t.Error("Expected error for non-existent git repo in test, but got nil")
	}
}
