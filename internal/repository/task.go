package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type TaskStatus string

const (
	StatusIdle           TaskStatus = "IDLE"
	StatusInProgress     TaskStatus = "IN_PROGRESS"
	StatusAwaitingCI     TaskStatus = "AWAITING_CI"
	StatusReadyForReview TaskStatus = "READY_FOR_REVIEW"
	StatusRevising       TaskStatus = "REVISING"
	StatusReviewPassed   TaskStatus = "REVIEW_PASSED"
	StatusFinished       TaskStatus = "FINISHED"
	StatusFailed         TaskStatus = "FAILED"
)

type Task struct {
	ID         string     `json:"id"`
	WorkflowID string     `json:"workflow_id"`
	Status     TaskStatus `json:"status"`
	TargetTags []string   `json:"target_tags"`
	Payload    string     `json:"payload"` // 儲存 MR_ID, Repository 等上下文
	Result     string     `json:"result"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type TaskRepository interface {
	CreateTask(task *Task) error
	GetTask(id string) (*Task, error)
	ListTasksByTags(tags []string, status TaskStatus) ([]*Task, error)
	UpdateTaskStatus(id string, status TaskStatus, result string) error
}

type SQLiteTaskRepository struct {
	db *sql.DB
}

func NewSQLiteTaskRepository(dbPath string) (*SQLiteTaskRepository, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := createTable(db); err != nil {
		return nil, err
	}

	return &SQLiteTaskRepository{db: db}, nil
}

func createTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		workflow_id TEXT NOT NULL,
		status TEXT NOT NULL,
		target_tags TEXT NOT NULL,
		payload TEXT,
		result TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_tasks_status_tags ON tasks(status, target_tags);
	`
	_, err := db.Exec(query)
	return err
}

func (r *SQLiteTaskRepository) CreateTask(task *Task) error {
	if task.ID == "" {
		task.ID = uuid.New().String()
	}
	task.CreatedAt = time.Now()
	task.UpdatedAt = time.Now()

	tagsJSON, _ := json.Marshal(task.TargetTags)

	query := `INSERT INTO tasks (id, workflow_id, status, target_tags, payload, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.Exec(query, task.ID, task.WorkflowID, task.Status, string(tagsJSON), task.Payload, task.CreatedAt, task.UpdatedAt)
	return err
}

func (r *SQLiteTaskRepository) GetTask(id string) (*Task, error) {
	query := `SELECT id, workflow_id, status, target_tags, payload, result, created_at, updated_at FROM tasks WHERE id = ?`
	row := r.db.QueryRow(query, id)

	var t Task
	var tagsJSON string
	var result sql.NullString
	if err := row.Scan(&t.ID, &t.WorkflowID, &t.Status, &tagsJSON, &t.Payload, &result, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	_ = json.Unmarshal([]byte(tagsJSON), &t.TargetTags)
	t.Result = result.String
	return &t, nil
}

// ListTasksByTags 實作標籤過濾邏輯，這部分與特定查詢條件需保持一致
func (r *SQLiteTaskRepository) ListTasksByTags(tags []string, status TaskStatus) ([]*Task, error) {
	query := `SELECT id, workflow_id, status, target_tags, payload, result, created_at, updated_at 
	          FROM tasks WHERE status = ?`

	rows, err := r.db.Query(query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		var t Task
		var tagsJSON string
		var result sql.NullString
		if err := rows.Scan(&t.ID, &t.WorkflowID, &t.Status, &tagsJSON, &t.Payload, &result, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}

		_ = json.Unmarshal([]byte(tagsJSON), &t.TargetTags)
		t.Result = result.String

		// 簡單的標籤子集匹配邏輯
		if r.matchTags(t.TargetTags, tags) {
			tasks = append(tasks, &t)
		}
	}
	return tasks, nil
}

func (r *SQLiteTaskRepository) UpdateTaskStatus(id string, status TaskStatus, result string) error {
	query := `UPDATE tasks SET status = ?, result = ?, updated_at = ? WHERE id = ?`
	_, err := r.db.Exec(query, status, result, time.Now(), id)
	return err
}

func (r *SQLiteTaskRepository) matchTags(target []string, provided []string) bool {
	if len(target) == 0 {
		return true
	}
	tagMap := make(map[string]bool)
	for _, t := range provided {
		tagMap[t] = true
	}
	for _, t := range target {
		if !tagMap[t] {
			return false
		}
	}
	return true
}
