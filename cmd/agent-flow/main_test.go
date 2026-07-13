package main

import "testing"

func TestParseMergeRequestURL(t *testing.T) {
	projectPath, mrIID, err := parseMergeRequestURL(
		"https://git.efaipd.com",
		"https://git.efaipd.com/htsprout/categorized-push/efai-pd-smart-888plus-admin-api-service/-/merge_requests/1",
	)
	if err != nil {
		t.Fatalf("parseMergeRequestURL failed: %v", err)
	}
	if projectPath != "htsprout/categorized-push/efai-pd-smart-888plus-admin-api-service" {
		t.Fatalf("unexpected project path: %q", projectPath)
	}
	if mrIID != 1 {
		t.Fatalf("unexpected mr iid: %d", mrIID)
	}
}
