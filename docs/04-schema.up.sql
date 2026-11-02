-- RepoAtlas initial PostgreSQL schema. PostgreSQL 15+.
CREATE TABLE repositories (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    github_repository_id BIGINT NOT NULL UNIQUE,
    owner TEXT NOT NULL,
    name TEXT NOT NULL,
    default_branch TEXT NOT NULL,
    html_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT repositories_owner_not_empty CHECK (length(owner) > 0),
    CONSTRAINT repositories_name_not_empty CHECK (length(name) > 0)
);

CREATE TABLE analysis_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    commit_sha CHAR(40) NOT NULL,
    analyzer_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    stage TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    lease_expires_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    files_count INTEGER NOT NULL DEFAULT 0 CHECK (files_count >= 0),
    internal_edges_count INTEGER NOT NULL DEFAULT 0 CHECK (internal_edges_count >= 0),
    external_imports_count INTEGER NOT NULL DEFAULT 0 CHECK (external_imports_count >= 0),
    unresolved_imports_count INTEGER NOT NULL DEFAULT 0 CHECK (unresolved_imports_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT analysis_runs_sha_format CHECK (commit_sha ~ '^[0-9a-f]{40}$'),
    CONSTRAINT analysis_runs_unique_snapshot UNIQUE (repository_id, commit_sha, analyzer_version)
);

CREATE INDEX analysis_runs_queue_idx
    ON analysis_runs (status, lease_expires_at, created_at);

CREATE TABLE source_files (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    analysis_id UUID NOT NULL REFERENCES analysis_runs(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    functions_count INTEGER NOT NULL DEFAULT 0 CHECK (functions_count >= 0),
    classes_count INTEGER NOT NULL DEFAULT 0 CHECK (classes_count >= 0),
    in_degree INTEGER NOT NULL DEFAULT 0 CHECK (in_degree >= 0),
    out_degree INTEGER NOT NULL DEFAULT 0 CHECK (out_degree >= 0),
    is_entrypoint BOOLEAN NOT NULL DEFAULT FALSE,
    importance_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    CONSTRAINT source_files_path_nonempty CHECK (length(path) > 0),
    CONSTRAINT source_files_unique_path UNIQUE (analysis_id, path),
    CONSTRAINT source_files_run_id_unique UNIQUE (analysis_id, id)
);

CREATE TABLE import_references (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    analysis_id UUID NOT NULL REFERENCES analysis_runs(id) ON DELETE CASCADE,
    source_file_id BIGINT NOT NULL,
    target_file_id BIGINT,
    import_text TEXT NOT NULL,
    module_name TEXT NOT NULL DEFAULT '',
    line_number INTEGER NOT NULL CHECK (line_number > 0),
    classification TEXT NOT NULL
        CHECK (classification IN ('internal', 'external', 'unresolved')),
    resolution_note TEXT,
    CONSTRAINT import_references_source_same_run
        FOREIGN KEY (analysis_id, source_file_id)
        REFERENCES source_files (analysis_id, id) ON DELETE CASCADE,
    CONSTRAINT import_references_target_same_run
        FOREIGN KEY (analysis_id, target_file_id)
        REFERENCES source_files (analysis_id, id) ON DELETE CASCADE,
    CONSTRAINT import_references_target_classification
        CHECK ((classification = 'internal' AND target_file_id IS NOT NULL)
            OR (classification <> 'internal' AND target_file_id IS NULL))
);

CREATE INDEX import_references_run_idx ON import_references (analysis_id);
CREATE INDEX import_references_source_idx ON import_references (analysis_id, source_file_id);
CREATE INDEX import_references_target_idx ON import_references (analysis_id, target_file_id)
    WHERE target_file_id IS NOT NULL;
