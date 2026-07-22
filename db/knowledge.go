package db

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/entreya/nativestudio/knowledge"
)

func (d *DB) GetIndexSettings(ctx context.Context, workspaceID string, defaults knowledge.IndexSettings) (knowledge.IndexSettings, error) {
	var includes, excludes, sensitive string
	result := defaults
	err := d.QueryRowContext(ctx, `SELECT include_paths,exclude_paths,sensitive_patterns,maximum_file_size_bytes FROM project_index_settings WHERE workspace_id=?`, workspaceID).Scan(&includes, &excludes, &sensitive, &result.MaximumFileSizeBytes)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	_ = json.Unmarshal([]byte(includes), &result.IncludePaths)
	_ = json.Unmarshal([]byte(excludes), &result.ExcludePaths)
	_ = json.Unmarshal([]byte(sensitive), &result.SensitivePatterns)
	return result, nil
}
func (d *DB) SaveIndexSettings(ctx context.Context, workspaceID string, settings knowledge.IndexSettings) error {
	includes, _ := json.Marshal(settings.IncludePaths)
	excludes, _ := json.Marshal(settings.ExcludePaths)
	sensitive, _ := json.Marshal(settings.SensitivePatterns)
	_, err := d.ExecContext(ctx, `INSERT INTO project_index_settings(workspace_id,include_paths,exclude_paths,sensitive_patterns,maximum_file_size_bytes) VALUES(?,?,?,?,?) ON CONFLICT(workspace_id) DO UPDATE SET include_paths=excluded.include_paths,exclude_paths=excluded.exclude_paths,sensitive_patterns=excluded.sensitive_patterns,maximum_file_size_bytes=excluded.maximum_file_size_bytes,updated_at=CURRENT_TIMESTAMP`, workspaceID, string(includes), string(excludes), string(sensitive), settings.MaximumFileSizeBytes)
	return err
}

// ResetProjectKnowledge removes repository-derived knowledge so the next index
// is built entirely from the current files. Conversations, decisions, session
// learnings, and indexing settings are intentionally preserved.
func (d *DB) ResetProjectKnowledge(ctx context.Context, workspaceID string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tables := []string{
		"index_jobs",
		"knowledge_fts",
		"symbol_relationships",
		"summary_versions",
		"module_summaries",
		"project_summaries",
		"project_facts",
		"repository_files",
		"indexing_runs",
	}
	for _, table := range tables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE workspace_id=?", workspaceID); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	return tx.Commit()
}

func encodeVector(vector []float32) []byte {
	data := make([]byte, len(vector)*4)
	for i, value := range vector {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(value))
	}
	return data
}

func decodeVector(data []byte) []float32 {
	vector := make([]float32, len(data)/4)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return vector
}

func (d *DB) RepositoryFileHash(ctx context.Context, workspaceID, path string) (string, error) {
	var hash string
	err := d.QueryRowContext(ctx, `SELECT content_hash FROM repository_files WHERE workspace_id = ? AND path = ?`, workspaceID, path).Scan(&hash)
	return hash, err
}
func (d *DB) RepositoryPaths(ctx context.Context, workspaceID string) ([]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT path FROM repository_files WHERE workspace_id=?`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

func (d *DB) RepositoryManifest(ctx context.Context, workspaceID string) (map[string]knowledge.FileManifest, error) {
	rows, err := d.QueryContext(ctx, `SELECT path,content_hash,git_status,size_bytes,modified_at_ns FROM repository_files WHERE workspace_id=? AND status='active'`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]knowledge.FileManifest)
	for rows.Next() {
		var item knowledge.FileManifest
		if err := rows.Scan(&item.Path, &item.ContentHash, &item.GitStatus, &item.SizeBytes, &item.ModifiedAtNS); err != nil {
			return nil, err
		}
		result[item.Path] = item
	}
	return result, rows.Err()
}

func (d *DB) UpdateRepositoryFileMetadata(ctx context.Context, workspaceID, path, gitStatus string, sizeBytes, modifiedAtNS int64) error {
	_, err := d.ExecContext(ctx, `UPDATE repository_files SET size_bytes=?,modified_at_ns=?,git_status=?,indexed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		WHERE workspace_id=? AND path=? AND status='active'`, sizeBytes, modifiedAtNS, gitStatus, workspaceID, path)
	return err
}

func (d *DB) ReplaceIndexedFile(ctx context.Context, workspaceID string, file knowledge.RepositoryFile, symbols []knowledge.Symbol, chunks []knowledge.Chunk, summary *knowledge.FileSummary, embeddingModel string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO repository_files
		(id, workspace_id, path, language, size_bytes, modified_at_ns, content_hash, git_status, status, skip_reason, indexed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(workspace_id, path) DO UPDATE SET language=excluded.language, size_bytes=excluded.size_bytes,
		modified_at_ns=excluded.modified_at_ns,content_hash=excluded.content_hash,git_status=excluded.git_status,status='active', skip_reason='', indexed_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP`,
		file.ID, workspaceID, file.Path, file.Language, file.SizeBytes, file.ModifiedAtNS, file.ContentHash, file.GitStatus)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_facts SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE workspace_id=? AND id IN (SELECT fact_id FROM fact_sources WHERE path=? AND content_hash<>?)`, workspaceID, file.Path, file.ContentHash); err != nil {
		return err
	}
	modulePath := "."
	normalized := filepath.ToSlash(file.Path)
	if strings.Contains(normalized, "/") {
		modulePath = strings.Split(normalized, "/")[0]
	}
	if _, err := tx.ExecContext(ctx, `UPDATE module_summaries SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE workspace_id=? AND module_path=?`, workspaceID, modulePath); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_summaries SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE workspace_id=?`, workspaceID); err != nil {
		return err
	}
	var fileID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM repository_files WHERE workspace_id=? AND path=?`, workspaceID, file.Path).Scan(&fileID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_fts WHERE workspace_id=? AND path=?`, workspaceID, file.Path); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM symbols WHERE file_id=?`, fileID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM code_chunks WHERE file_id=?`, fileID); err != nil {
		return err
	}
	for i := range symbols {
		symbols[i].FileID = fileID
		_, err = tx.ExecContext(ctx, `INSERT INTO symbols
			(id,workspace_id,file_id,path,symbol_name,qualified_name,symbol_type,signature,start_line,end_line,content_hash,summary)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, symbols[i].ID, workspaceID, fileID, symbols[i].Path, symbols[i].Name,
			symbols[i].QualifiedName, symbols[i].Type, symbols[i].Signature, symbols[i].StartLine, symbols[i].EndLine,
			symbols[i].ContentHash, symbols[i].Summary)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_fts(workspace_id,kind,entity_id,path,symbol,signature,content) VALUES(?,'symbol',?,?,?,?,?)`, workspaceID, symbols[i].ID, symbols[i].Path, symbols[i].Name, symbols[i].Signature, symbols[i].Summary); err != nil {
			return err
		}
		if symbols[i].Summary != "" {
			target := symbols[i].Path + "#" + symbols[i].QualifiedName
			version := 1
			model := ""
			if summary != nil {
				model = summary.Model
			}
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM summary_versions WHERE workspace_id=? AND summary_type='symbol' AND target_id=?`, workspaceID, target).Scan(&version); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO summary_versions(id,workspace_id,summary_type,target_id,version,input_hash,prompt_version,model,status) VALUES(?,?,'symbol',?,?,?,?,?,'valid')`, NewID(), workspaceID, target, version, symbols[i].ContentHash, "symbol-v1", model); err != nil {
				return err
			}
		}
	}
	for i := range chunks {
		chunks[i].FileID = fileID
		_, err = tx.ExecContext(ctx, `INSERT INTO code_chunks
			(id,workspace_id,file_id,symbol_id,path,start_line,end_line,content,content_hash,token_estimate)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, chunks[i].ID, workspaceID, fileID, nullable(chunks[i].SymbolID), chunks[i].Path,
			chunks[i].StartLine, chunks[i].EndLine, chunks[i].Content, chunks[i].ContentHash, chunks[i].TokenEstimate)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_fts(workspace_id,kind,entity_id,path,symbol,signature,content) VALUES(?,'chunk',?,?,?,'',?)`, workspaceID, chunks[i].ID, chunks[i].Path, "", chunks[i].Content); err != nil {
			return err
		}
		if len(chunks[i].Embedding) > 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO embeddings
				(id,workspace_id,chunk_id,model,dimensions,embedding,content_hash) VALUES(?,?,?,?,?,?,?)`,
				NewID(), workspaceID, chunks[i].ID, embeddingModel, len(chunks[i].Embedding), encodeVector(chunks[i].Embedding), chunks[i].ContentHash)
			if err != nil {
				return err
			}
		}
	}
	if summary != nil && summary.Summary != "" {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM summary_versions WHERE workspace_id=? AND summary_type='file' AND target_id=?`, workspaceID, file.Path).Scan(&summary.Version); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE file_summaries SET status='stale', updated_at=CURRENT_TIMESTAMP WHERE workspace_id=? AND path=? AND content_hash<>?`, workspaceID, file.Path, file.ContentHash); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO file_summaries(id,workspace_id,file_id,path,summary,content_hash,version,model,status)
			VALUES(?,?,?,?,?,?,?,?, 'active') ON CONFLICT(workspace_id,path,content_hash) DO UPDATE SET summary=excluded.summary, model=excluded.model, status='active', updated_at=CURRENT_TIMESTAMP`,
			summary.ID, workspaceID, fileID, file.Path, summary.Summary, file.ContentHash, summary.Version, summary.Model)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO summary_versions(id,workspace_id,summary_type,target_id,version,input_hash,prompt_version,model,status) VALUES(?,?, 'file',?,?,?,?,?,'valid')`, NewID(), workspaceID, file.Path, summary.Version, file.ContentHash, "file-v1", summary.Model); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveFileEnrichment attaches optional model-generated data to an already
// persisted structural index. The content hash guard prevents a slow model
// response for an older file version from overwriting a newer index entry.
func (d *DB) SaveFileEnrichment(ctx context.Context, workspaceID, path, contentHash, embeddingModel string, chunks []knowledge.Chunk, summary *knowledge.FileSummary) (bool, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var fileID, currentHash string
	if err := tx.QueryRowContext(ctx, `SELECT id,content_hash FROM repository_files WHERE workspace_id=? AND path=? AND status='active'`, workspaceID, path).Scan(&fileID, &currentHash); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if currentHash != contentHash {
		return false, nil
	}

	for _, chunk := range chunks {
		if len(chunk.Embedding) == 0 {
			continue
		}
		var persisted int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_chunks WHERE id=? AND workspace_id=? AND content_hash=? AND status='active'`, chunk.ID, workspaceID, chunk.ContentHash).Scan(&persisted); err != nil {
			return false, err
		}
		if persisted == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO embeddings
			(id,workspace_id,chunk_id,model,dimensions,embedding,content_hash,status)
			VALUES(?,?,?,?,?,?,?,'active')
			ON CONFLICT(chunk_id,model) DO UPDATE SET dimensions=excluded.dimensions,embedding=excluded.embedding,
			content_hash=excluded.content_hash,status='active',updated_at=CURRENT_TIMESTAMP`,
			NewID(), workspaceID, chunk.ID, embeddingModel, len(chunk.Embedding), encodeVector(chunk.Embedding), chunk.ContentHash); err != nil {
			return false, err
		}
	}

	if summary != nil && summary.Summary != "" {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM summary_versions WHERE workspace_id=? AND summary_type='file' AND target_id=?`, workspaceID, path).Scan(&summary.Version); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE file_summaries SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE workspace_id=? AND path=? AND content_hash<>?`, workspaceID, path, contentHash); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO file_summaries(id,workspace_id,file_id,path,summary,content_hash,version,model,status)
			VALUES(?,?,?,?,?,?,?,?, 'active')
			ON CONFLICT(workspace_id,path,content_hash) DO UPDATE SET summary=excluded.summary,model=excluded.model,
			status='active',updated_at=CURRENT_TIMESTAMP`, summary.ID, workspaceID, fileID, path, summary.Summary, contentHash, summary.Version, summary.Model); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO summary_versions(id,workspace_id,summary_type,target_id,version,input_hash,prompt_version,model,status)
			VALUES(?,?,'file',?,?,?,?,?,'valid')`, NewID(), workspaceID, path, summary.Version, contentHash, "file-v1", summary.Model); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (d *DB) EnqueueIndexJob(ctx context.Context, workspaceID, runID, path, contentHash, jobType string, priority int) error {
	_, err := d.ExecContext(ctx, `INSERT INTO index_jobs(id,workspace_id,run_id,path,content_hash,job_type,priority,status)
		VALUES(?,?,?,?,?,?,?,'queued')
		ON CONFLICT(workspace_id,path,job_type) DO UPDATE SET run_id=excluded.run_id,content_hash=excluded.content_hash,
		priority=excluded.priority,status='queued',attempts=0,last_error='',available_at=CURRENT_TIMESTAMP,
		started_at=NULL,completed_at=NULL,updated_at=CURRENT_TIMESTAMP`, NewID(), workspaceID, nullable(runID), path, contentHash, jobType, priority)
	return err
}

func (d *DB) RequeueRunningIndexJobs(ctx context.Context, workspaceID, jobType string) error {
	_, err := d.ExecContext(ctx, `UPDATE index_jobs SET status='queued',started_at=NULL,available_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		WHERE workspace_id=? AND job_type=? AND status='running'`, workspaceID, jobType)
	return err
}

func (d *DB) ClaimIndexJob(ctx context.Context, workspaceID, jobType string) (*knowledge.IndexJob, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var job knowledge.IndexJob
	err = tx.QueryRowContext(ctx, `SELECT id,workspace_id,COALESCE(run_id,''),path,content_hash,job_type,priority,attempts
		FROM index_jobs WHERE workspace_id=? AND job_type=? AND status='queued' AND available_at<=CURRENT_TIMESTAMP
		ORDER BY priority ASC,created_at ASC LIMIT 1`, workspaceID, jobType).
		Scan(&job.ID, &job.WorkspaceID, &job.RunID, &job.Path, &job.ContentHash, &job.JobType, &job.Priority, &job.Attempts)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE index_jobs SET status='running',attempts=attempts+1,started_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='queued'`, job.ID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	job.Attempts++
	return &job, nil
}

func (d *DB) CompleteIndexJob(ctx context.Context, id, contentHash, status, message string) error {
	_, err := d.ExecContext(ctx, `UPDATE index_jobs SET status=?,last_error=?,completed_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=? AND content_hash=?`, status, message, id, contentHash)
	return err
}

func (d *DB) PendingIndexJobCount(ctx context.Context, workspaceID, jobType string) (int, error) {
	var count int
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM index_jobs WHERE workspace_id=? AND job_type=? AND status IN ('queued','running')`, workspaceID, jobType).Scan(&count)
	return count, err
}

func (d *DB) LoadFileForEnrichment(ctx context.Context, workspaceID, path, contentHash string) (knowledge.RepositoryFile, []knowledge.Symbol, []knowledge.Chunk, bool, error) {
	var file knowledge.RepositoryFile
	err := d.QueryRowContext(ctx, `SELECT id,workspace_id,path,language,size_bytes,modified_at_ns,content_hash,git_status,status
		FROM repository_files WHERE workspace_id=? AND path=? AND content_hash=? AND status='active'`, workspaceID, path, contentHash).
		Scan(&file.ID, &file.WorkspaceID, &file.Path, &file.Language, &file.SizeBytes, &file.ModifiedAtNS, &file.ContentHash, &file.GitStatus, &file.Status)
	if err == sql.ErrNoRows {
		return file, nil, nil, false, nil
	}
	if err != nil {
		return file, nil, nil, false, err
	}
	rows, err := d.QueryContext(ctx, `SELECT id,file_id,path,symbol_name,qualified_name,symbol_type,signature,start_line,end_line,content_hash,summary
		FROM symbols WHERE workspace_id=? AND path=? AND content_hash=? AND status='active' ORDER BY start_line`, workspaceID, path, contentHash)
	if err != nil {
		return file, nil, nil, false, err
	}
	var symbols []knowledge.Symbol
	for rows.Next() {
		var item knowledge.Symbol
		if err := rows.Scan(&item.ID, &item.FileID, &item.Path, &item.Name, &item.QualifiedName, &item.Type, &item.Signature, &item.StartLine, &item.EndLine, &item.ContentHash, &item.Summary); err != nil {
			rows.Close()
			return file, nil, nil, false, err
		}
		symbols = append(symbols, item)
	}
	if err := rows.Close(); err != nil {
		return file, nil, nil, false, err
	}
	rows, err = d.QueryContext(ctx, `SELECT id,file_id,COALESCE(symbol_id,''),path,start_line,end_line,content,content_hash,token_estimate
		FROM code_chunks WHERE workspace_id=? AND path=? AND status='active' ORDER BY start_line`, workspaceID, path)
	if err != nil {
		return file, nil, nil, false, err
	}
	defer rows.Close()
	var chunks []knowledge.Chunk
	for rows.Next() {
		var item knowledge.Chunk
		if err := rows.Scan(&item.ID, &item.FileID, &item.SymbolID, &item.Path, &item.StartLine, &item.EndLine, &item.Content, &item.ContentHash, &item.TokenEstimate); err != nil {
			return file, nil, nil, false, err
		}
		chunks = append(chunks, item)
	}
	return file, symbols, chunks, true, rows.Err()
}

func (d *DB) ReplaceFileRelationships(ctx context.Context, workspaceID, path string, imports []string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM symbol_relationships WHERE workspace_id=? AND source_path=?`, workspaceID, path); err != nil {
		return err
	}
	for _, target := range imports {
		if _, err := tx.ExecContext(ctx, `INSERT INTO symbol_relationships(id,workspace_id,source_path,target_name,relationship_type,confidence,status) VALUES(?,?,?,?,'imports',1,'active')`, NewID(), workspaceID, path, target); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) RemoveIndexedFile(ctx context.Context, workspaceID, path string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM knowledge_fts WHERE workspace_id=? AND path=?`, workspaceID, path); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM repository_files WHERE workspace_id=? AND path=?`, workspaceID, path); err != nil {
		return err
	}
	_, _ = tx.ExecContext(ctx, `UPDATE project_facts SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE workspace_id=? AND id IN(SELECT fact_id FROM fact_sources WHERE path=?)`, workspaceID, path)
	modulePath := "."
	normalized := filepath.ToSlash(path)
	if strings.Contains(normalized, "/") {
		modulePath = strings.Split(normalized, "/")[0]
	}
	_, _ = tx.ExecContext(ctx, `UPDATE module_summaries SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE workspace_id=? AND module_path=?`, workspaceID, modulePath)
	_, _ = tx.ExecContext(ctx, `UPDATE project_summaries SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE workspace_id=? AND status='active'`, workspaceID)
	return tx.Commit()
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (d *DB) SaveProjectSummary(ctx context.Context, workspaceID string, summary knowledge.ProjectSummary) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM summary_versions WHERE workspace_id=? AND summary_type='project' AND target_id=?`, workspaceID, workspaceID).Scan(&summary.Version); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE project_summaries SET status='stale', updated_at=CURRENT_TIMESTAMP WHERE workspace_id=?`, workspaceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO project_summaries(id,workspace_id,summary,source_hash,version,model,status) VALUES(?,?,?,?,?,?, 'active')`, summary.ID, workspaceID, summary.Summary, summary.SourceHash, summary.Version, summary.Model); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO summary_versions(id,workspace_id,summary_type,target_id,version,input_hash,prompt_version,model,status) VALUES(?,?, 'project',?,?,?,?,?,'valid')`, NewID(), workspaceID, workspaceID, summary.Version, summary.SourceHash, "project-v1", summary.Model); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) SaveModuleSummary(ctx context.Context, workspaceID string, summary knowledge.ModuleSummary) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM summary_versions WHERE workspace_id=? AND summary_type='module' AND target_id=?`, workspaceID, summary.ModulePath).Scan(&summary.Version); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE module_summaries SET status='stale',updated_at=CURRENT_TIMESTAMP WHERE workspace_id=? AND module_path=?`, workspaceID, summary.ModulePath); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO module_summaries(id,workspace_id,module_path,summary,source_hash,version,model,status) VALUES(?,?,?,?,?,?,?,'active')`, summary.ID, workspaceID, summary.ModulePath, summary.Summary, summary.SourceHash, summary.Version, summary.Model); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO summary_versions(id,workspace_id,summary_type,target_id,version,input_hash,prompt_version,model,status) VALUES(?,?,'module',?,?,?,?,?,'valid')`, NewID(), workspaceID, summary.ModulePath, summary.Version, summary.SourceHash, "module-v1", summary.Model); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) ModuleSummaries(ctx context.Context, workspaceID string) ([]knowledge.ModuleSummary, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,module_path,summary,source_hash,version,model,status FROM module_summaries WHERE workspace_id=? AND status='active' ORDER BY module_path`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []knowledge.ModuleSummary
	for rows.Next() {
		var item knowledge.ModuleSummary
		if err := rows.Scan(&item.ID, &item.ModulePath, &item.Summary, &item.SourceHash, &item.Version, &item.Model, &item.Status); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *DB) FileSummaries(ctx context.Context, workspaceID string, limit int) ([]knowledge.FileSummary, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,path,summary,content_hash,version,model,status FROM file_summaries WHERE workspace_id=? AND status='active' ORDER BY path LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []knowledge.FileSummary
	for rows.Next() {
		var item knowledge.FileSummary
		if err := rows.Scan(&item.ID, &item.Path, &item.Summary, &item.ContentHash, &item.Version, &item.Model, &item.Status); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (d *DB) LatestIndexStatus(ctx context.Context, workspaceID string) (knowledge.IndexStatus, error) {
	var item knowledge.IndexStatus
	item.WorkspaceID = workspaceID
	err := d.QueryRowContext(ctx, `SELECT id,status,processed,total,skipped,errors,started_at,completed_at,error FROM indexing_runs WHERE workspace_id=? ORDER BY started_at DESC LIMIT 1`, workspaceID).
		Scan(&item.RunID, &item.Status, &item.Processed, &item.Total, &item.Skipped, &item.Errors, &item.StartedAt, &item.CompletedAt, &item.Error)
	if err == sql.ErrNoRows {
		item.Status = "not_indexed"
		err = nil
	}
	if err != nil {
		return item, err
	}
	item.EnrichmentRemaining, _ = d.PendingIndexJobCount(ctx, workspaceID, "file_enrichment")
	if item.EnrichmentRemaining > 0 {
		item.EnrichmentStatus = "running"
	} else {
		item.EnrichmentStatus = "completed"
	}
	return item, nil
}

func (d *DB) KnowledgeOverview(ctx context.Context, workspaceID string) (knowledge.Overview, error) {
	result := knowledge.Overview{WorkspaceID: workspaceID, Languages: map[string]int{}, Files: []knowledge.FileSummary{}, Facts: []knowledge.Fact{}}
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN status='stale' THEN 1 ELSE 0 END),0) FROM repository_files WHERE workspace_id=?`, workspaceID).Scan(&result.IndexedFiles, &result.StaleCount); err != nil {
		return result, err
	}
	_ = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM code_chunks WHERE workspace_id=? AND status='active'`, workspaceID).Scan(&result.IndexedChunks)
	_ = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM symbols WHERE workspace_id=? AND status='active'`, workspaceID).Scan(&result.SymbolCount)
	rows, err := d.QueryContext(ctx, `SELECT language,COUNT(*) FROM repository_files WHERE workspace_id=? AND status='active' GROUP BY language`, workspaceID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var lang string
		var count int
		if rows.Scan(&lang, &count) == nil {
			result.Languages[lang] = count
		}
	}
	rows.Close()
	result.Files, _ = d.FileSummaries(ctx, workspaceID, 200)
	result.Modules, _ = d.ModuleSummaries(ctx, workspaceID)
	var ps knowledge.ProjectSummary
	if d.QueryRowContext(ctx, `SELECT id,summary,source_hash,version,model,status FROM project_summaries WHERE workspace_id=? AND status='active' ORDER BY updated_at DESC LIMIT 1`, workspaceID).Scan(&ps.ID, &ps.Summary, &ps.SourceHash, &ps.Version, &ps.Model, &ps.Status) == nil {
		result.Project = &ps
	}
	result.Status, _ = d.LatestIndexStatus(ctx, workspaceID)
	result.Facts, _ = d.ListFacts(ctx, workspaceID)
	result.Symbols, _ = d.ListImportantSymbols(ctx, workspaceID, 100)
	result.Decisions, _ = d.ListDecisions(ctx, workspaceID)
	result.Learnings, _ = d.ListLearnings(ctx, workspaceID, 30)
	result.Errors, _ = d.ListIndexErrors(ctx, workspaceID, 50)
	result.SkippedFiles, _ = d.ListIndexSkips(ctx, workspaceID, 100)
	return result, nil
}
func (d *DB) ListImportantSymbols(ctx context.Context, workspaceID string, limit int) ([]knowledge.Symbol, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,file_id,path,symbol_name,qualified_name,symbol_type,signature,start_line,end_line,content_hash,summary FROM symbols WHERE workspace_id=? AND status='active' ORDER BY CASE symbol_type WHEN 'class' THEN 0 WHEN 'interface' THEN 1 WHEN 'trait' THEN 2 WHEN 'type' THEN 3 ELSE 4 END,path,start_line LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []knowledge.Symbol
	for rows.Next() {
		var item knowledge.Symbol
		if err := rows.Scan(&item.ID, &item.FileID, &item.Path, &item.Name, &item.QualifiedName, &item.Type, &item.Signature, &item.StartLine, &item.EndLine, &item.ContentHash, &item.Summary); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) ListDecisions(ctx context.Context, workspaceID string) ([]knowledge.Decision, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,COALESCE(session_id,''),decision,reason,status,created_at FROM project_decisions WHERE workspace_id=? ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []knowledge.Decision
	for rows.Next() {
		var item knowledge.Decision
		if err := rows.Scan(&item.ID, &item.SessionID, &item.Decision, &item.Reason, &item.Status, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (d *DB) ListLearnings(ctx context.Context, workspaceID string, limit int) ([]knowledge.Learning, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,COALESCE(session_id,''),run_id,learning,patch_status,created_at FROM session_learnings WHERE workspace_id=? ORDER BY created_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []knowledge.Learning
	for rows.Next() {
		var item knowledge.Learning
		if err := rows.Scan(&item.ID, &item.SessionID, &item.RunID, &item.Learning, &item.PatchStatus, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) UpsertVerifiedFact(ctx context.Context, workspaceID string, fact knowledge.Fact) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM project_facts WHERE workspace_id=? AND fact=? LIMIT 1`, workspaceID, fact.Fact).Scan(&id)
	if err == sql.ErrNoRows {
		id = NewID()
		_, err = tx.ExecContext(ctx, `INSERT INTO project_facts(id,workspace_id,category,fact,confidence,status,last_verified_at) VALUES(?,?,?,?,?,'verified',CURRENT_TIMESTAMP)`, id, workspaceID, fact.Category, fact.Fact, fact.Confidence)
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE project_facts SET category=?,confidence=?,status='verified',last_verified_at=CURRENT_TIMESTAMP,updated_at=CURRENT_TIMESTAMP WHERE id=?`, fact.Category, fact.Confidence, id)
	}
	if err != nil {
		return err
	}
	for _, source := range fact.Sources {
		_, err = tx.ExecContext(ctx, `INSERT INTO fact_sources(id,fact_id,path,symbol_name,start_line,end_line,content_hash) SELECT ?,?,?,?,?,?,? WHERE NOT EXISTS(SELECT 1 FROM fact_sources WHERE fact_id=? AND path=? AND content_hash=?)`, NewID(), id, source.Path, source.SymbolName, nullableInt(source.StartLine), nullableInt(source.EndLine), source.ContentHash, id, source.Path, source.ContentHash)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}
func (d *DB) ListFacts(ctx context.Context, workspaceID string) ([]knowledge.Fact, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,category,fact,confidence,status,pinned,is_rule,last_verified_at FROM project_facts WHERE workspace_id=? ORDER BY pinned DESC,status,updated_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []knowledge.Fact
	for rows.Next() {
		var fact knowledge.Fact
		if err := rows.Scan(&fact.ID, &fact.Category, &fact.Fact, &fact.Confidence, &fact.Status, &fact.Pinned, &fact.IsRule, &fact.LastVerifiedAt); err != nil {
			return nil, err
		}
		sources, sourceErr := d.QueryContext(ctx, `SELECT path,symbol_name,COALESCE(start_line,0),COALESCE(end_line,0),content_hash FROM fact_sources WHERE fact_id=?`, fact.ID)
		if sourceErr != nil {
			return nil, sourceErr
		}
		for sources.Next() {
			var source knowledge.FactSource
			if sources.Scan(&source.Path, &source.SymbolName, &source.StartLine, &source.EndLine, &source.ContentHash) == nil {
				fact.Sources = append(fact.Sources, source)
			}
		}
		sources.Close()
		if fact.Sources == nil {
			fact.Sources = []knowledge.FactSource{}
		}
		result = append(result, fact)
	}
	return result, rows.Err()
}
func (d *DB) UpdateFact(ctx context.Context, workspaceID, id, text, status string, pinned, isRule bool) error {
	_, err := d.ExecContext(ctx, `UPDATE project_facts SET fact=?,status=?,pinned=?,is_rule=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND workspace_id=?`, text, status, pinned, isRule, id, workspaceID)
	return err
}
func (d *DB) DeleteFact(ctx context.Context, workspaceID, id string) error {
	_, err := d.ExecContext(ctx, `DELETE FROM project_facts WHERE id=? AND workspace_id=?`, id, workspaceID)
	return err
}

func ftsKnowledgeQuery(terms []string) string {
	var clauses []string
	for _, term := range terms {
		if len([]rune(term)) < 3 {
			continue
		}
		clauses = append(clauses, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	return strings.Join(clauses, " OR ")
}

func scoreSymbolCandidate(path, name, summary, signature, source string, start, end int, terms []string) (knowledge.Candidate, bool) {
	hay := strings.ToLower(path + " " + name + " " + summary + " " + signature)
	score := 0.0
	var reasons []string
	for _, term := range terms {
		if strings.EqualFold(name, term) {
			score += 85
			reasons = append(reasons, "exact_symbol_match")
		} else if strings.Contains(hay, term) {
			score += 18
			reasons = append(reasons, "symbol_keyword_match")
		}
		if strings.Contains(strings.ToLower(path), term) {
			score += 25
			reasons = append(reasons, "filename_match")
		}
	}
	if score == 0 {
		return knowledge.Candidate{}, false
	}
	content := summary
	if content == "" {
		content = source
	}
	return knowledge.Candidate{Kind: "symbol", Path: path, Symbol: name, StartLine: start, EndLine: end, Content: content, Score: score, Reasons: reasons}, true
}

func (d *DB) searchStructuralKnowledgeFTS(ctx context.Context, workspaceID string, terms []string, limit int) ([]knowledge.Candidate, error) {
	match := ftsKnowledgeQuery(terms)
	if match == "" {
		return d.searchStructuralKnowledgeLegacy(ctx, workspaceID, terms)
	}
	candidateLimit := limit * 8
	if candidateLimit < 32 {
		candidateLimit = 32
	}
	rows, err := d.QueryContext(ctx, `SELECT s.path,s.symbol_name,s.start_line,s.end_line,s.summary,s.signature,
		COALESCE((SELECT c.content FROM code_chunks c WHERE c.symbol_id=s.id AND c.status='active' ORDER BY c.start_line LIMIT 1),'')
		FROM knowledge_fts JOIN symbols s ON s.id=knowledge_fts.entity_id
		WHERE knowledge_fts MATCH ? AND knowledge_fts.workspace_id=? AND knowledge_fts.kind='symbol' AND s.status='active'
		ORDER BY bm25(knowledge_fts,0,0,0,2,8,4,1) LIMIT ?`, match, workspaceID, candidateLimit)
	if err != nil {
		return nil, err
	}
	var result []knowledge.Candidate
	for rows.Next() {
		var path, name, summary, signature, source string
		var start, end int
		if err := rows.Scan(&path, &name, &start, &end, &summary, &signature, &source); err != nil {
			rows.Close()
			return nil, err
		}
		if candidate, ok := scoreSymbolCandidate(path, name, summary, signature, source, start, end, terms); ok {
			candidate.Reasons = append(candidate.Reasons, "fts_candidate")
			result = append(result, candidate)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	rows, err = d.QueryContext(ctx, `SELECT c.path,c.start_line,c.end_line,c.content
		FROM knowledge_fts JOIN code_chunks c ON c.id=knowledge_fts.entity_id
		WHERE knowledge_fts MATCH ? AND knowledge_fts.workspace_id=? AND knowledge_fts.kind='chunk' AND c.status='active'
		ORDER BY bm25(knowledge_fts,0,0,0,2,8,4,1) LIMIT ?`, match, workspaceID, candidateLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate knowledge.Candidate
		if err := rows.Scan(&candidate.Path, &candidate.StartLine, &candidate.EndLine, &candidate.Content); err != nil {
			return nil, err
		}
		lowerPath := strings.ToLower(candidate.Path)
		lowerContent := strings.ToLower(candidate.Content)
		for _, term := range terms {
			if strings.Contains(lowerPath, term) {
				candidate.Score += 35
				candidate.Reasons = append(candidate.Reasons, "filename_match")
			}
			if strings.Contains(lowerContent, term) {
				candidate.Score += 12
				candidate.Reasons = append(candidate.Reasons, "code_keyword_match")
			}
		}
		if candidate.Score > 0 {
			candidate.Kind = "code"
			candidate.Reasons = append(candidate.Reasons, "fts_candidate")
			result = append(result, candidate)
		}
	}
	return result, rows.Err()
}

func (d *DB) searchStructuralKnowledgeLegacy(ctx context.Context, workspaceID string, terms []string) ([]knowledge.Candidate, error) {
	rows, err := d.QueryContext(ctx, `SELECT s.path,s.symbol_name,s.start_line,s.end_line,s.summary,s.signature,
		COALESCE((SELECT c.content FROM code_chunks c WHERE c.symbol_id=s.id AND c.status='active' ORDER BY c.start_line LIMIT 1),'')
		FROM symbols s WHERE s.workspace_id=? AND s.status='active'`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []knowledge.Candidate
	for rows.Next() {
		var path, name, summary, signature, source string
		var start, end int
		if err := rows.Scan(&path, &name, &start, &end, &summary, &signature, &source); err != nil {
			return nil, err
		}
		if candidate, ok := scoreSymbolCandidate(path, name, summary, signature, source, start, end, terms); ok {
			result = append(result, candidate)
		}
	}
	return result, rows.Err()
}

func (d *DB) SearchKnowledge(ctx context.Context, workspaceID, query string, limit int) ([]knowledge.Candidate, error) {
	terms := knowledgeQueryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	type scored struct{ c knowledge.Candidate }
	var all []scored
	structural, err := d.searchStructuralKnowledgeFTS(ctx, workspaceID, terms, limit)
	if err != nil {
		structural, err = d.searchStructuralKnowledgeLegacy(ctx, workspaceID, terms)
		if err != nil {
			return nil, err
		}
	}
	for _, candidate := range structural {
		all = append(all, scored{candidate})
	}
	moduleRows, moduleErr := d.QueryContext(ctx, `SELECT module_path,summary FROM module_summaries WHERE workspace_id=? AND status='active'`, workspaceID)
	if moduleErr == nil {
		for moduleRows.Next() {
			var path, summary string
			if moduleRows.Scan(&path, &summary) != nil {
				continue
			}
			score := 0.0
			var reasons []string
			lower := strings.ToLower(path + " " + summary)
			for _, term := range terms {
				if strings.Contains(lower, term) {
					score += 30
					reasons = append(reasons, "module_summary_match")
				}
			}
			if score > 0 {
				all = append(all, scored{knowledge.Candidate{Kind: "module_summary", Path: path, Content: summary, Score: score, Reasons: reasons}})
			}
		}
		moduleRows.Close()
	}
	var projectSummary string
	if d.QueryRowContext(ctx, `SELECT summary FROM project_summaries WHERE workspace_id=? AND status='active' ORDER BY updated_at DESC LIMIT 1`, workspaceID).Scan(&projectSummary) == nil {
		all = append(all, scored{knowledge.Candidate{Kind: "project_summary", Content: projectSummary, Score: 5, Reasons: []string{"project_summary_fallback"}}})
	}
	factRows, factErr := d.QueryContext(ctx, `SELECT fact,pinned,is_rule FROM project_facts WHERE workspace_id=? AND status='verified'`, workspaceID)
	if factErr == nil {
		for factRows.Next() {
			var fact string
			var pinned, isRule bool
			if factRows.Scan(&fact, &pinned, &isRule) != nil {
				continue
			}
			score := 0.0
			var reasons []string
			lower := strings.ToLower(fact)
			for _, term := range terms {
				if strings.Contains(lower, term) {
					score += 30
					reasons = append(reasons, "verified_fact_match")
				}
			}
			if pinned {
				score += 20
				reasons = append(reasons, "pinned_project_memory")
			}
			if isRule {
				score += 30
				reasons = append(reasons, "explicit_project_rule")
			}
			if score > 0 {
				all = append(all, scored{knowledge.Candidate{Kind: "fact", Content: fact, Score: score, Reasons: reasons}})
			}
		}
		factRows.Close()
	}
	decisionRows, decisionErr := d.QueryContext(ctx, `SELECT decision,status FROM project_decisions WHERE workspace_id=? ORDER BY created_at DESC LIMIT 100`, workspaceID)
	if decisionErr == nil {
		for decisionRows.Next() {
			var decision, status string
			if decisionRows.Scan(&decision, &status) != nil {
				continue
			}
			matched := false
			lower := strings.ToLower(decision)
			for _, term := range terms {
				if strings.Contains(lower, term) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if status == "accepted" {
				all = append(all, scored{knowledge.Candidate{Kind: "decision", Content: decision, Score: 50, Reasons: []string{"previous_accepted_decision"}}})
			} else if status == "rejected" {
				all = append(all, scored{knowledge.Candidate{Kind: "decision", Content: "Do not repeat this previously rejected approach: " + decision, Score: 80, Reasons: []string{"rejected_approach_guardrail", "ranking_penalty_applied_to_related_code"}}})
			}
		}
		decisionRows.Close()
	}
	changeRows, changeErr := d.QueryContext(ctx, `SELECT path,summary FROM file_changes WHERE workspace_id=? AND status='approved' ORDER BY updated_at DESC LIMIT 100`, workspaceID)
	if changeErr == nil {
		for changeRows.Next() {
			var path, summary string
			if changeRows.Scan(&path, &summary) != nil {
				continue
			}
			lower := strings.ToLower(path + " " + summary)
			matched := false
			for _, term := range terms {
				if strings.Contains(lower, term) {
					matched = true
					break
				}
			}
			if matched {
				all = append(all, scored{knowledge.Candidate{Kind: "accepted_change", Path: path, Content: "Previously accepted change: " + summary, Score: 50, Reasons: []string{"previous_related_accepted_change"}}})
			}
		}
		changeRows.Close()
	}
	rows, err := d.QueryContext(ctx, `SELECT path,summary FROM file_summaries WHERE workspace_id=? AND status='active'`, workspaceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var path, summary string
		if rows.Scan(&path, &summary) != nil {
			continue
		}
		hay := strings.ToLower(path + " " + summary)
		score := 0.0
		var reasons []string
		for _, term := range terms {
			if strings.Contains(strings.ToLower(path), term) {
				score += 55
				reasons = append(reasons, "filename_match")
			}
			if strings.Contains(hay, term) {
				score += 30
				reasons = append(reasons, "summary_keyword_relevance")
			}
		}
		if score > 0 {
			all = append(all, scored{knowledge.Candidate{Kind: "file_summary", Path: path, Content: summary, Score: score, Reasons: reasons}})
		}
	}
	rows.Close()
	sort.SliceStable(all, func(i, j int) bool { return all[i].c.Score > all[j].c.Score })
	if len(all) > limit {
		all = all[:limit]
	}
	result := make([]knowledge.Candidate, len(all))
	for i := range all {
		result[i] = all[i].c
	}
	return result, nil
}

var ignoredKnowledgeTerms = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "can": true, "could": true,
	"do": true, "for": true, "hello": true, "hey": true, "hi": true, "how": true,
	"i": true, "is": true, "it": true, "me": true, "of": true, "on": true, "please": true,
	"the": true, "this": true, "to": true, "what": true, "would": true, "you": true,
}

func knowledgeQueryTerms(query string) []string {
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	})
	seen := map[string]bool{}
	terms := make([]string, 0, len(words))
	for _, word := range words {
		if ignoredKnowledgeTerms[word] || seen[word] {
			continue
		}
		if len([]rune(word)) < 3 && word != "go" && word != "js" && word != "ts" && word != "ui" && word != "db" && word != "ai" {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
	}
	return terms
}

func (d *DB) KnowledgeForPath(ctx context.Context, workspaceID, path string) (knowledge.Candidate, error) {
	path = strings.TrimPrefix(filepath.ToSlash(path), "/")
	var content string
	var start, end int
	err := d.QueryRowContext(ctx, `SELECT content,start_line,end_line FROM code_chunks WHERE workspace_id=? AND path=? AND status='active' ORDER BY CASE WHEN symbol_id IS NULL THEN 1 ELSE 0 END,start_line LIMIT 1`, workspaceID, path).Scan(&content, &start, &end)
	if err != nil {
		return knowledge.Candidate{}, err
	}
	return knowledge.Candidate{Kind: "code", Path: path, StartLine: start, EndLine: end, Content: content}, nil
}

func (d *DB) StartIndexRun(ctx context.Context, workspaceID string, total int) (string, error) {
	id := NewID()
	_, err := d.ExecContext(ctx, `INSERT INTO indexing_runs(id,workspace_id,status,total) VALUES(?,?,'running',?)`, id, workspaceID, total)
	return id, err
}
func (d *DB) UpdateIndexRun(ctx context.Context, id, status string, processed, skipped, errors int, message string) error {
	completed := any(nil)
	if status != "running" {
		completed = time.Now()
	}
	_, err := d.ExecContext(ctx, `UPDATE indexing_runs SET status=?,processed=?,skipped=?,errors=?,error=?,completed_at=? WHERE id=?`, status, processed, skipped, errors, message, completed, id)
	return err
}
func (d *DB) SaveIndexError(ctx context.Context, runID, workspaceID, path, stage string, cause error) error {
	_, err := d.ExecContext(ctx, `INSERT INTO indexing_errors(id,run_id,workspace_id,path,stage,error) VALUES(?,?,?,?,?,?)`, NewID(), runID, workspaceID, path, stage, cause.Error())
	return err
}
func (d *DB) SaveIndexSkip(ctx context.Context, runID, workspaceID, path, reason string) error {
	_, err := d.ExecContext(ctx, `INSERT INTO indexing_skips(id,run_id,workspace_id,path,reason) VALUES(?,?,?,?,?)`, NewID(), runID, workspaceID, path, reason)
	return err
}
func (d *DB) ListIndexErrors(ctx context.Context, workspaceID string, limit int) ([]knowledge.IndexError, error) {
	rows, err := d.QueryContext(ctx, `SELECT path,stage,error,created_at FROM indexing_errors WHERE workspace_id=? ORDER BY created_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []knowledge.IndexError
	for rows.Next() {
		var item knowledge.IndexError
		if err := rows.Scan(&item.Path, &item.Stage, &item.Error, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (d *DB) ListIndexSkips(ctx context.Context, workspaceID string, limit int) ([]knowledge.IndexSkip, error) {
	rows, err := d.QueryContext(ctx, `SELECT path,reason,created_at FROM indexing_skips WHERE workspace_id=? ORDER BY created_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []knowledge.IndexSkip
	for rows.Next() {
		var item knowledge.IndexSkip
		if err := rows.Scan(&item.Path, &item.Reason, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, aa, bb float64
	for i := range a {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		aa += av * av
		bb += bv * bv
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return dot / (math.Sqrt(aa) * math.Sqrt(bb))
}
// maxEmbeddingScanRows bounds the worst-case cost of SearchEmbeddings. There is
// no vector index here — every candidate row is decoded and scored with cosine
// similarity in application code — so without a cap a very large indexed repo
// would do an ever-growing full scan on every chat turn that has semantic
// intent. This trades completeness (a few distant chunks may not be scanned)
// for a predictable worst case; revisit with a real ANN index if repos this
// large become common.
const maxEmbeddingScanRows = 20000

func (d *DB) SearchEmbeddings(ctx context.Context, workspaceID, model string, query []float32, limit int) ([]knowledge.Candidate, error) {
	rows, err := d.QueryContext(ctx, `SELECT c.path,c.start_line,c.end_line,c.content,e.embedding FROM embeddings e JOIN code_chunks c ON c.id=e.chunk_id WHERE e.workspace_id=? AND e.model=? AND e.status='active' AND c.status='active' LIMIT ?`, workspaceID, model, maxEmbeddingScanRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []knowledge.Candidate
	for rows.Next() {
		var c knowledge.Candidate
		var raw []byte
		if rows.Scan(&c.Path, &c.StartLine, &c.EndLine, &c.Content, &raw) != nil {
			continue
		}
		sim := cosine(query, decodeVector(raw))
		if sim < 0.45 {
			continue
		}
		c.Kind = "code"
		c.Score = sim * 35
		c.Reasons = []string{fmt.Sprintf("embedding_similarity:%.3f", sim)}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
