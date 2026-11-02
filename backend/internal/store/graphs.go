package store

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

func (s *PostgresStore) CompleteAnalysis(ctx context.Context, id string, leaseOwner string, output graph.BuildOutput) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin complete tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Verify ownership and status under row lock to prevent stale worker overwrite
	var currentStatus string
	var currentOwner *string
	err = tx.QueryRow(ctx, `
		SELECT status, lease_owner
		FROM analysis_runs
		WHERE id = $1::uuid
		FOR UPDATE
	`, id).Scan(&currentStatus, &currentOwner)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrLeaseLost
	}
	if err != nil {
		return fmt.Errorf("lock analysis_run for completion: %w", err)
	}
	if currentStatus != string(domain.StatusRunning) {
		return domain.ErrLeaseLost
	}
	if leaseOwner != "" && (currentOwner == nil || *currentOwner != leaseOwner) {
		return domain.ErrLeaseLost
	}

	// Clear any remnants from a previous timed-out attempt of the same analysis_id
	if _, err := tx.Exec(ctx, `DELETE FROM import_references WHERE analysis_id = $1::uuid`, id); err != nil {
		return fmt.Errorf("delete old import_references: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM source_files WHERE analysis_id = $1::uuid`, id); err != nil {
		return fmt.Errorf("delete old source_files: %w", err)
	}

	pathToFileID := make(map[string]int64, len(output.Files))
	for _, f := range output.Files {
		var fileID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO source_files (
				analysis_id, path, functions_count, classes_count,
				in_degree, out_degree, is_entrypoint, importance_score
			)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id
		`, id, f.Path, f.FunctionsCount, f.ClassesCount, f.InDegree, f.OutDegree, f.IsEntrypoint, f.ImportanceScore).Scan(&fileID)
		if err != nil {
			return fmt.Errorf("insert source_file %s: %w", f.Path, err)
		}
		pathToFileID[f.Path] = fileID
	}

	for _, imp := range output.Imports {
		srcID, ok := pathToFileID[imp.SourcePath]
		if !ok {
			return fmt.Errorf("unknown source file path %q in import reference", imp.SourcePath)
		}
		var targetIDPtr *int64
		classification := imp.Classification
		if classification == domain.ImportInternal {
			dstID, dstOK := pathToFileID[imp.TargetPath]
			if !dstOK {
				return fmt.Errorf("unknown target file path %q in internal import reference", imp.TargetPath)
			}
			targetIDPtr = &dstID
		}

		lineNum := imp.LineNumber
		if lineNum <= 0 {
			lineNum = 1
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO import_references (
				analysis_id, source_file_id, target_file_id,
				import_text, module_name, line_number, classification, resolution_note
			)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8)
		`, id, srcID, targetIDPtr, imp.ImportText, imp.ModuleName, lineNum, string(classification), imp.ResolutionNote)
		if err != nil {
			return fmt.Errorf("insert import_reference in %s: %w", imp.SourcePath, err)
		}
	}

	tag, err := tx.Exec(ctx, `
		UPDATE analysis_runs
		SET status = 'completed',
		    stage = 'completed',
		    files_count = $2,
		    internal_edges_count = $3,
		    external_imports_count = $4,
		    unresolved_imports_count = $5,
		    error_code = NULL,
		    error_message = NULL,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    completed_at = now()
		WHERE id = $1::uuid
		  AND status = 'running'
		  AND ($6 = '' OR lease_owner = $6)
	`, id, output.FilesCount, output.InternalEdgesCount, output.ExternalImportsCount, output.UnresolvedImportsCount, leaseOwner)
	if err != nil {
		return fmt.Errorf("update analysis_run completed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrLeaseLost
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit complete tx: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetAnalysisGraph(ctx context.Context, id string) (domain.AnalysisRun, []domain.SourceFile, []graph.Edge, error) {
	run, err := s.GetAnalysis(ctx, id)
	if err != nil {
		return domain.AnalysisRun{}, nil, nil, err
	}

	if run.Status != domain.StatusCompleted {
		return domain.AnalysisRun{}, nil, nil, domain.NewAppError(
			domain.ErrCodeAnalysisNotReady,
			http.StatusConflict,
			fmt.Sprintf("解析はまだ完了していません（現在の状態: %s）", run.Status),
			nil,
		)
	}

	fileRows, err := s.pool.Query(ctx, `
		SELECT id, analysis_id::text, path, functions_count, classes_count,
		       in_degree, out_degree, is_entrypoint, importance_score
		FROM source_files
		WHERE analysis_id = $1::uuid
		ORDER BY path ASC
	`, id)
	if err != nil {
		return domain.AnalysisRun{}, nil, nil, fmt.Errorf("query source_files: %w", err)
	}
	defer fileRows.Close()

	var files []domain.SourceFile
	for fileRows.Next() {
		var f domain.SourceFile
		if err := fileRows.Scan(
			&f.ID, &f.AnalysisID, &f.Path, &f.FunctionsCount, &f.ClassesCount,
			&f.InDegree, &f.OutDegree, &f.IsEntrypoint, &f.ImportanceScore,
		); err != nil {
			return domain.AnalysisRun{}, nil, nil, fmt.Errorf("scan source_file: %w", err)
		}
		files = append(files, f)
	}
	if err := fileRows.Err(); err != nil {
		return domain.AnalysisRun{}, nil, nil, fmt.Errorf("iterate source_files: %w", err)
	}

	edgeRows, err := s.pool.Query(ctx, `
		SELECT DISTINCT sf_src.path, sf_dst.path
		FROM import_references ir
		JOIN source_files sf_src ON sf_src.analysis_id = ir.analysis_id AND sf_src.id = ir.source_file_id
		JOIN source_files sf_dst ON sf_dst.analysis_id = ir.analysis_id AND sf_dst.id = ir.target_file_id
		WHERE ir.analysis_id = $1::uuid
		  AND ir.classification = 'internal'
		  AND ir.target_file_id IS NOT NULL
		  AND ir.source_file_id <> ir.target_file_id
		ORDER BY sf_src.path ASC, sf_dst.path ASC
	`, id)
	if err != nil {
		return domain.AnalysisRun{}, nil, nil, fmt.Errorf("query internal edges: %w", err)
	}
	defer edgeRows.Close()

	var edges []graph.Edge
	for edgeRows.Next() {
		var e graph.Edge
		if err := edgeRows.Scan(&e.SourcePath, &e.TargetPath); err != nil {
			return domain.AnalysisRun{}, nil, nil, fmt.Errorf("scan edge: %w", err)
		}
		e.Kind = "import"
		edges = append(edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return domain.AnalysisRun{}, nil, nil, fmt.Errorf("iterate edges: %w", err)
	}

	return run, files, edges, nil
}
