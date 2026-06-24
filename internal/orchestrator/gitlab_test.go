package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHttpGitLabRepository_FetchPendingTodos(t *testing.T) {
	// 建立 Mock Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/todos" {
			t.Errorf("Expected path /api/v4/todos, got %s", r.URL.Path)
		}

		todos := []struct {
			ID      int `json:"id"`
			Project struct {
				PathWithNamespace string `json:"path_with_namespace"`
			} `json:"project"`
			Target struct {
				IID   int    `json:"iid"`
				Title string `json:"title"`
				State string `json:"state"`
			} `json:"target"`
		}{
			{
				ID: 123,
				Project: struct {
					PathWithNamespace string `json:"path_with_namespace"`
				}{PathWithNamespace: "group/project"},
				Target: struct {
					IID   int    `json:"iid"`
					Title string `json:"title"`
					State string `json:"state"`
				}{IID: 456, Title: "Test MR", State: "opened"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(todos)
	}))
	defer server.Close()

	repo := NewHttpGitLabRepository(server.URL, "fake-token")
	todos, err := repo.FetchPendingTodos(context.Background())
	if err != nil {
		t.Fatalf("Failed to fetch todos: %v", err)
	}

	if len(todos) != 1 {
		t.Errorf("Expected 1 todo, got %d", len(todos))
	}

	if todos[0].ID != 123 || todos[0].MergeRequest.IID != 456 {
		t.Errorf("Data mismatch in fetched todo")
	}
}

func TestHttpGitLabRepository_FetchMergeRequestPipelines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/group%2Fproject/merge_requests/456/pipelines" {
			t.Errorf("Expected path, got %s", r.URL.Path)
		}
		pipelines := []struct {
			ID     int    `json:"id"`
			Status string `json:"status"`
		}{
			{ID: 789, Status: "success"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pipelines)
	}))
	defer server.Close()

	repo := NewHttpGitLabRepository(server.URL, "fake-token")
	pipelines, err := repo.FetchMergeRequestPipelines(context.Background(), "group/project", 456)
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	if len(pipelines) != 1 || pipelines[0].Status != "success" {
		t.Errorf("Mismatch in fetched pipelines")
	}
}

