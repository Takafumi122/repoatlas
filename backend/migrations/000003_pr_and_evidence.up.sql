-- RepoAtlas Phase 5: Pull Request Impact Analysis & Explainable Static Analysis schema.

CREATE TABLE IF NOT EXISTS import_evidence (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    analysis_id UUID NOT NULL REFERENCES analysis_runs(id) ON DELETE CASCADE,
    source_file_id BIGINT NOT NULL,
    target_file_id BIGINT,
    import_statement TEXT NOT NULL,
    module_name TEXT NOT NULL DEFAULT '',
    start_line INTEGER NOT NULL CHECK (start_line > 0),
    start_column INTEGER NOT NULL CHECK (start_column > 0),
    end_line INTEGER NOT NULL CHECK (end_line >= start_line),
    end_column INTEGER NOT NULL CHECK (end_column > 0),
    import_kind TEXT NOT NULL CHECK (import_kind IN ('absolute', 'relative')),
    resolution_status TEXT NOT NULL CHECK (resolution_status IN ('internal', 'external', 'unresolved')),
    resolution_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT import_evidence_source_same_run
        FOREIGN KEY (analysis_id, source_file_id)
        REFERENCES source_files (analysis_id, id) ON DELETE CASCADE,
    CONSTRAINT import_evidence_target_same_run
        FOREIGN KEY (analysis_id, target_file_id)
        REFERENCES source_files (analysis_id, id) ON DELETE CASCADE,
    CONSTRAINT import_evidence_target_classification
        CHECK ((resolution_status = 'internal' AND target_file_id IS NOT NULL)
            OR (resolution_status <> 'internal' AND target_file_id IS NULL))
);

CREATE INDEX IF NOT EXISTS import_evidence_run_idx ON import_evidence (analysis_id);
CREATE INDEX IF NOT EXISTS import_evidence_edge_idx ON import_evidence (analysis_id, source_file_id, target_file_id)
    WHERE target_file_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS import_evidence_unresolved_idx ON import_evidence (analysis_id, resolution_status)
    WHERE resolution_status = 'unresolved';

CREATE TABLE IF NOT EXISTS pull_request_analyses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    pull_number INTEGER NOT NULL CHECK (pull_number > 0),
    title TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'open',
    base_sha CHAR(40) NOT NULL,
    head_sha CHAR(40) NOT NULL,
    base_ref TEXT NOT NULL DEFAULT '',
    head_ref TEXT NOT NULL DEFAULT '',
    analyzer_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    stage TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    head_analysis_id UUID REFERENCES analysis_runs(id) ON DELETE SET NULL,
    base_analysis_id UUID REFERENCES analysis_runs(id) ON DELETE SET NULL,
    error_code TEXT,
    error_message TEXT,
    changed_files_count INTEGER NOT NULL DEFAULT 0 CHECK (changed_files_count >= 0),
    direct_impact_count INTEGER NOT NULL DEFAULT 0 CHECK (direct_impact_count >= 0),
    indirect_impact_count INTEGER NOT NULL DEFAULT 0 CHECK (indirect_impact_count >= 0),
    total_affected_count INTEGER NOT NULL DEFAULT 0 CHECK (total_affected_count >= 0),
    unanalyzed_files_count INTEGER NOT NULL DEFAULT 0 CHECK (unanalyzed_files_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    CONSTRAINT pr_analyses_base_sha_format CHECK (base_sha ~ '^[0-9a-f]{40}$'),
    CONSTRAINT pr_analyses_head_sha_format CHECK (head_sha ~ '^[0-9a-f]{40}$'),
    CONSTRAINT pr_analyses_unique_snapshot UNIQUE (repository_id, pull_number, base_sha, head_sha, analyzer_version)
);

CREATE INDEX IF NOT EXISTS pr_analyses_queue_idx
    ON pull_request_analyses (status, lease_expires_at, created_at);

CREATE TABLE IF NOT EXISTS pull_request_files (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pr_analysis_id UUID NOT NULL REFERENCES pull_request_analyses(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    previous_path TEXT,
    status TEXT NOT NULL CHECK (status IN ('added', 'modified', 'removed', 'renamed')),
    additions INTEGER NOT NULL DEFAULT 0 CHECK (additions >= 0),
    deletions INTEGER NOT NULL DEFAULT 0 CHECK (deletions >= 0),
    changes INTEGER NOT NULL DEFAULT 0 CHECK (changes >= 0),
    is_python BOOLEAN NOT NULL DEFAULT FALSE,
    is_analyzed BOOLEAN NOT NULL DEFAULT FALSE,
    unanalyzed_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pr_files_unique_path UNIQUE (pr_analysis_id, path)
);

CREATE INDEX IF NOT EXISTS pr_files_pr_idx ON pull_request_files (pr_analysis_id);
