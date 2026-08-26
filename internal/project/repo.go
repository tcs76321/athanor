package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tcs76321/athanor/internal/ids"
)

// Create makes a project with its first goal and task, atomically.
func (r *Repo) Create(ctx context.Context, name, archetype, goal string, criteria []string) (Project, Task, error) {
	if !ValidArchetype(archetype) {
		return Project{}, Task{}, fmt.Errorf("invalid archetype %q (§6.2: text|code|document|data|media)", archetype)
	}
	if err := validateGoalText(goal); err != nil {
		return Project{}, Task{}, err
	}
	if name == "" {
		return Project{}, Task{}, fmt.Errorf("project name must not be empty")
	}
	projectID, goalID, taskID := ids.New(), ids.New(), ids.New()
	criteriaJSON, err := marshalCriteria(criteria)
	if err != nil {
		return Project{}, Task{}, err
	}

	tx, err := r.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return Project{}, Task{}, fmt.Errorf("beginning project create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects (id, name, archetype, goal) VALUES (?, ?, ?, ?)`,
		projectID, name, archetype, goal,
	); err != nil {
		return Project{}, Task{}, fmt.Errorf("inserting project: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO goals (id, project_id, text) VALUES (?, ?, ?)`,
		goalID, projectID, goal,
	); err != nil {
		return Project{}, Task{}, fmt.Errorf("inserting goal: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tasks (id, project_id, goal_id, title, acceptance_criteria_json) VALUES (?, ?, ?, ?, ?)`,
		taskID, projectID, goalID, goal, criteriaJSON,
	); err != nil {
		return Project{}, Task{}, fmt.Errorf("inserting task: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Project{}, Task{}, fmt.Errorf("committing project create: %w", err)
	}

	p, err := r.Get(ctx, projectID)
	if err != nil {
		return Project{}, Task{}, err
	}
	task, err := r.Task(ctx, taskID)
	if err != nil {
		return Project{}, Task{}, err
	}
	return p, task, nil
}

// SubmitGoal adds a new goal (and its M1 task) to an existing project.
func (r *Repo) SubmitGoal(ctx context.Context, projectID, goalText string, criteria []string) (Task, error) {
	if err := validateGoalText(goalText); err != nil {
		return Task{}, err
	}
	if _, err := r.Get(ctx, projectID); err != nil {
		return Task{}, err
	}
	goalID, taskID := ids.New(), ids.New()
	criteriaJSON, err := marshalCriteria(criteria)
	if err != nil {
		return Task{}, err
	}
	if _, err := r.store.DB().ExecContext(ctx,
		`INSERT INTO goals (id, project_id, text) VALUES (?, ?, ?)`,
		goalID, projectID, goalText,
	); err != nil {
		return Task{}, fmt.Errorf("inserting goal: %w", err)
	}
	if _, err := r.store.DB().ExecContext(ctx,
		`INSERT INTO tasks (id, project_id, goal_id, title, acceptance_criteria_json) VALUES (?, ?, ?, ?, ?)`,
		taskID, projectID, goalID, goalText, criteriaJSON,
	); err != nil {
		return Task{}, fmt.Errorf("inserting task: %w", err)
	}
	return r.Task(ctx, taskID)
}

// Get loads one project by ID.
func (r *Repo) Get(ctx context.Context, id string) (Project, error) {
	var p Project
	var createdAt string
	err := r.store.DB().QueryRowContext(ctx,
		`SELECT id, name, archetype, goal, status, created_at FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Archetype, &p.Goal, &p.Status, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Project{}, fmt.Errorf("loading project: %w", err)
	}
	var perr error
	if p.CreatedAt, perr = time.Parse(time.RFC3339, createdAt); perr != nil {
		return Project{}, fmt.Errorf("parsing project timestamp %q: %w", createdAt, perr)
	}
	return p, nil
}

// Task loads one task by ID.
func (r *Repo) Task(ctx context.Context, id string) (Task, error) {
	var t Task
	var criteriaJSON string
	err := r.store.DB().QueryRowContext(ctx,
		`SELECT id, project_id, COALESCE(goal_id, ''), title, COALESCE(description, ''), status,
		        acceptance_criteria_json
		 FROM tasks WHERE id = ?`, id,
	).Scan(&t.ID, &t.ProjectID, &t.GoalID, &t.Title, &t.Description, &t.Status, &criteriaJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("%w (task): %s", ErrNotFound, id)
	}
	if err != nil {
		return Task{}, fmt.Errorf("loading task: %w", err)
	}
	if err := json.Unmarshal([]byte(criteriaJSON), &t.Criteria); err != nil {
		return Task{}, fmt.Errorf("decoding criteria for task %s: %w", id, err)
	}
	return t, nil
}
