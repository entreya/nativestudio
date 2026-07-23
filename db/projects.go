package db

import (
	"time"
)

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (d *DB) CreateProject(id, name, path, description string) (*Project, error) {
	_, err := d.Exec(`
		INSERT INTO projects (id, name, path, description)
		VALUES (?, ?, ?, ?)
	`, id, name, path, description)
	if err != nil {
		return nil, err
	}
	return d.GetProject(id)
}

func (d *DB) ListProjects() ([]Project, error) {
	rows, err := d.Query(`SELECT id, name, path, description, created_at, updated_at FROM projects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Path, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (d *DB) GetProject(id string) (*Project, error) {
	var p Project
	err := d.QueryRow(`
		SELECT id, name, path, description, created_at, updated_at
		FROM projects WHERE id = ?
	`, id).Scan(&p.ID, &p.Name, &p.Path, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetProjectByPath looks up a project by its workspace path. Returns
// (nil, sql.ErrNoRows) when no project is registered for that path.
func (d *DB) GetProjectByPath(path string) (*Project, error) {
	var p Project
	err := d.QueryRow(`
		SELECT id, name, path, description, created_at, updated_at
		FROM projects WHERE path = ?
	`, path).Scan(&p.ID, &p.Name, &p.Path, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *DB) DeleteProject(id string) error {
	_, err := d.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

func (d *DB) UpdateProjectPath(id, path string) error {
	_, err := d.Exec(`UPDATE projects SET path = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, path, id)
	return err
}
