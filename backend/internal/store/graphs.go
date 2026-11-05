package store

import (
	"context"
	"encoding/json"
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
	_, _ = tx.Exec(ctx, `DELETE FROM import_evidence WHERE analysis_id = $1::uuid`, id)
	if _, err := tx.Exec(ctx, `DELETE FROM import_references WHERE analysis_id = $1::uuid`, id); err != nil {
		return fmt.Errorf("delete old import_references: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM source_files WHERE analysis_id = $1::uuid`, id); err != nil {
		return fmt.Errorf("delete old source_files: %w", err)
	}

	pathToFileID := make(map[string]int64, len(output.Files))
	for _, f := range output.Files {
		var metaJSON []byte
		if len(f.Files) > 0 {
			metaJSON, _ = json.Marshal(f.Files)
		}

		nodeKind := f.NodeKind
		if nodeKind == "" {
			nodeKind = "file"
		}
		lang := f.Language
		if lang == "" {
			lang = "python"
		}
		filesCount := f.FilesCount
		if filesCount <= 0 {
			filesCount = 1
		}

		var fileID int64
		err := tx.QueryRow(ctx, `
			INSERT INTO source_files (
				analysis_id, path, functions_count, classes_count,
				in_degree, out_degree, is_entrypoint, importance_score,
				node_kind, language, module_path, package_import_path,
				directory_path, package_name, files_count, methods_count,
				types_count, metadata
			)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
			RETURNING id
		`, id, f.Path, f.FunctionsCount, f.ClassesCount, f.InDegree, f.OutDegree, f.IsEntrypoint, f.ImportanceScore,
			nodeKind, lang, f.ModulePath, f.PackageImportPath, f.DirectoryPath, f.PackageName, filesCount, f.MethodsCount, f.TypesCount, metaJSON).Scan(&fileID)
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
		colNum := imp.ColumnNumber
		if colNum <= 0 {
			colNum = 1
		}
		modName := imp.ModuleName
		if modName == "" {
			modName = imp.TargetPackage
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO import_references (
				analysis_id, source_file_id, target_file_id,
				import_text, module_name, line_number, classification, resolution_note,
				source_file, alias, column_number, build_constraints
			)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, id, srcID, targetIDPtr, imp.ImportText, modName, lineNum, string(classification), imp.ResolutionNote,
			imp.SourceFile, imp.Alias, colNum, imp.BuildConstraints)
		if err != nil {
			return fmt.Errorf("insert import_reference in %s: %w", imp.SourcePath, err)
		}

		startLine := imp.StartLine
		if startLine <= 0 {
			startLine = lineNum
		}
		startCol := imp.StartColumn
		if startCol <= 0 {
			startCol = colNum
		}
		endLine := imp.EndLine
		if endLine < startLine {
			endLine = startLine
		}
		endCol := imp.EndColumn
		if endCol <= 0 {
			endCol = startCol + len(imp.ImportText)
		}
		importKind := imp.ImportKind
		if importKind == "" {
			importKind = "absolute"
		}
		resReason := imp.ResolutionReason
		if resReason == "" && imp.ResolutionNote != nil {
			resReason = *imp.ResolutionNote
		}

		_, _ = tx.Exec(ctx, `
			INSERT INTO import_evidence (
				analysis_id, source_file_id, target_file_id,
				import_statement, module_name, start_line, start_column, end_line, end_column,
				import_kind, resolution_status, resolution_reason
			)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, id, srcID, targetIDPtr, imp.ImportText, imp.ModuleName, startLine, startCol, endLine, endCol, importKind, string(classification), resReason)
	}

	lang := output.Language
	if lang == "" {
		lang = "python"
	}
	granularity := output.GraphGranularity
	if granularity == "" {
		if lang == "go" {
			granularity = "package"
		} else {
			granularity = "file"
		}
	}

	tag, err := tx.Exec(ctx, `
		UPDATE analysis_runs
		SET status = 'completed',
		    stage = 'completed',
		    language = $7,
		    graph_granularity = $8,
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
	`, id, output.FilesCount, output.InternalEdgesCount, output.ExternalImportsCount, output.UnresolvedImportsCount, leaseOwner, lang, granularity)
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
		       in_degree, out_degree, is_entrypoint, importance_score,
		       COALESCE(node_kind, 'file'), COALESCE(language, 'python'),
		       COALESCE(module_path, ''), COALESCE(package_import_path, ''),
		       COALESCE(directory_path, ''), COALESCE(package_name, ''),
		       COALESCE(files_count, 1), COALESCE(methods_count, 0),
		       COALESCE(types_count, 0), metadata
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
		var metaJSON []byte
		if err := fileRows.Scan(
			&f.ID, &f.AnalysisID, &f.Path, &f.FunctionsCount, &f.ClassesCount,
			&f.InDegree, &f.OutDegree, &f.IsEntrypoint, &f.ImportanceScore,
			&f.NodeKind, &f.Language,
			&f.ModulePath, &f.PackageImportPath,
			&f.DirectoryPath, &f.PackageName,
			&f.FilesCount, &f.MethodsCount,
			&f.TypesCount, &metaJSON,
		); err != nil {
			return domain.AnalysisRun{}, nil, nil, fmt.Errorf("scan source_file: %w", err)
		}
		if len(metaJSON) > 0 {
			_ = json.Unmarshal(metaJSON, &f.Files)
		}
		files = append(files, f)
	}
	if err := fileRows.Err(); err != nil {
		return domain.AnalysisRun{}, nil, nil, fmt.Errorf("iterate source_files: %w", err)
	}

	edgeRows, err := s.pool.Query(ctx, `
		SELECT sf_src.path, sf_dst.path, COUNT(ir.id) AS evidence_count
		FROM import_references ir
		JOIN source_files sf_src ON sf_src.analysis_id = ir.analysis_id AND sf_src.id = ir.source_file_id
		JOIN source_files sf_dst ON sf_dst.analysis_id = ir.analysis_id AND sf_dst.id = ir.target_file_id
		WHERE ir.analysis_id = $1::uuid
		  AND ir.classification = 'internal'
		  AND ir.target_file_id IS NOT NULL
		  AND ir.source_file_id <> ir.target_file_id
		GROUP BY sf_src.path, sf_dst.path
		ORDER BY sf_src.path ASC, sf_dst.path ASC
	`, id)
	if err != nil {
		return domain.AnalysisRun{}, nil, nil, fmt.Errorf("query internal edges: %w", err)
	}
	defer edgeRows.Close()

	var edges []graph.Edge
	for edgeRows.Next() {
		var e graph.Edge
		if err := edgeRows.Scan(&e.SourcePath, &e.TargetPath, &e.EvidenceCount); err != nil {
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

func (s *PostgresStore) GetImportReferences(ctx context.Context, id string) ([]domain.ImportReference, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ir.id, ir.analysis_id::text, ir.source_file_id, ir.target_file_id,
		       sf_src.path AS source_path, COALESCE(sf_dst.path, '') AS target_path,
		       ir.import_text, ir.module_name, ir.line_number, ir.classification,
		       ir.resolution_note, COALESCE(ir.source_file, ''),
		       COALESCE(ir.alias, ''), COALESCE(ir.column_number, 1),
		       COALESCE(ir.build_constraints, '')
		FROM import_references ir
		JOIN source_files sf_src ON sf_src.analysis_id = ir.analysis_id AND sf_src.id = ir.source_file_id
		LEFT JOIN source_files sf_dst ON sf_dst.analysis_id = ir.analysis_id AND sf_dst.id = ir.target_file_id
		WHERE ir.analysis_id = $1::uuid
		ORDER BY sf_src.path ASC, ir.line_number ASC
	`, id)
	if err != nil {
		return nil, fmt.Errorf("query import_references: %w", err)
	}
	defer rows.Close()

	var refs []domain.ImportReference
	for rows.Next() {
		var ref domain.ImportReference
		var targetID *int64
		if err := rows.Scan(
			&ref.ID, &ref.AnalysisID, &ref.SourceFileID, &targetID,
			&ref.SourcePath, &ref.TargetPath,
			&ref.ImportText, &ref.ModuleName, &ref.LineNumber, &ref.Classification,
			&ref.ResolutionNote, &ref.SourceFile,
			&ref.Alias, &ref.ColumnNumber,
			&ref.BuildConstraints,
		); err != nil {
			return nil, fmt.Errorf("scan import_reference: %w", err)
		}
		ref.TargetFileID = targetID
		ref.SourcePackage = ref.SourcePath
		ref.TargetPackage = ref.TargetPath
		if ref.TargetPackage == "" {
			ref.TargetPackage = ref.ModuleName
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *PostgresStore) GetImportEvidences(ctx context.Context, analysisID string, sourcePath, targetPath string, unresolvedOnly bool) ([]domain.ImportEvidence, error) {
	query := `
		SELECT
			e.id,
			e.analysis_id::text,
			src.path AS source_path,
			COALESCE(tgt.path, '') AS target_path,
			e.import_statement,
			e.module_name,
			e.start_line,
			e.start_column,
			e.end_line,
			e.end_column,
			e.import_kind,
			e.resolution_status,
			e.resolution_reason,
			repo.owner,
			repo.name,
			ar.commit_sha
		FROM import_evidence e
		JOIN analysis_runs ar ON ar.id = e.analysis_id
		JOIN repositories repo ON repo.id = ar.repository_id
		JOIN source_files src ON src.analysis_id = e.analysis_id AND src.id = e.source_file_id
		LEFT JOIN source_files tgt ON tgt.analysis_id = e.analysis_id AND tgt.id = e.target_file_id
		WHERE e.analysis_id = $1::uuid
	`
	args := []any{analysisID}

	if unresolvedOnly {
		query += ` AND e.resolution_status = 'unresolved'`
	} else if sourcePath != "" && targetPath != "" {
		args = append(args, sourcePath, targetPath)
		query += fmt.Sprintf(` AND src.path = $%d AND tgt.path = $%d`, len(args)-1, len(args))
	} else if sourcePath != "" {
		args = append(args, sourcePath)
		query += fmt.Sprintf(` AND src.path = $%d`, len(args))
	}

	query += ` ORDER BY src.path ASC, e.start_line ASC, e.start_column ASC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query import_evidence: %w", err)
	}
	defer rows.Close()

	var evidences []domain.ImportEvidence
	for rows.Next() {
		var ev domain.ImportEvidence
		var owner, repo, sha string
		if err := rows.Scan(
			&ev.ID,
			&ev.AnalysisID,
			&ev.SourcePath,
			&ev.TargetPath,
			&ev.ImportStatement,
			&ev.ModuleName,
			&ev.StartLine,
			&ev.StartColumn,
			&ev.EndLine,
			&ev.EndColumn,
			&ev.ImportKind,
			&ev.ResolutionStatus,
			&ev.ResolutionReason,
			&owner,
			&repo,
			&sha,
		); err != nil {
			return nil, fmt.Errorf("scan import_evidence: %w", err)
		}
		if owner != "" && repo != "" && sha != "" && ev.SourcePath != "" && ev.StartLine > 0 {
			ev.GitHubURL = fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s#L%d", owner, repo, sha, ev.SourcePath, ev.StartLine)
		}
		evidences = append(evidences, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows import_evidence: %w", err)
	}
	if evidences == nil {
		evidences = []domain.ImportEvidence{}
	}
	return evidences, nil
}
