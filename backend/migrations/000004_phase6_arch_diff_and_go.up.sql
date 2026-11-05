-- Phase 6: Architecture Diff & Go Language Support Migration

ALTER TABLE analysis_runs
    ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'python',
    ADD COLUMN IF NOT EXISTS graph_granularity TEXT NOT NULL DEFAULT 'file';

ALTER TABLE source_files
    ADD COLUMN IF NOT EXISTS node_kind TEXT NOT NULL DEFAULT 'file',
    ADD COLUMN IF NOT EXISTS language TEXT NOT NULL DEFAULT 'python',
    ADD COLUMN IF NOT EXISTS module_path TEXT,
    ADD COLUMN IF NOT EXISTS package_import_path TEXT,
    ADD COLUMN IF NOT EXISTS directory_path TEXT,
    ADD COLUMN IF NOT EXISTS package_name TEXT,
    ADD COLUMN IF NOT EXISTS files_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS methods_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS types_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS metadata JSONB;

ALTER TABLE import_references
    ADD COLUMN IF NOT EXISTS source_file TEXT,
    ADD COLUMN IF NOT EXISTS alias TEXT,
    ADD COLUMN IF NOT EXISTS column_number INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS build_constraints TEXT,
    ADD COLUMN IF NOT EXISTS evidence_metadata JSONB;

CREATE TABLE IF NOT EXISTS architecture_diffs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    base_sha CHAR(40) NOT NULL,
    head_sha CHAR(40) NOT NULL,
    base_analysis_id UUID REFERENCES analysis_runs(id) ON DELETE SET NULL,
    head_analysis_id UUID REFERENCES analysis_runs(id) ON DELETE SET NULL,
    analyzer_version TEXT NOT NULL,
    diff_algorithm_version TEXT NOT NULL DEFAULT 'v1.0.0',
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    stage TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    error_code TEXT,
    error_message TEXT,
    diff_summary JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT architecture_diffs_base_sha_format CHECK (base_sha ~ '^[0-9a-f]{40}$'),
    CONSTRAINT architecture_diffs_head_sha_format CHECK (head_sha ~ '^[0-9a-f]{40}$'),
    CONSTRAINT architecture_diffs_unique_comparison UNIQUE (repository_id, base_sha, head_sha, analyzer_version, diff_algorithm_version)
);

CREATE INDEX IF NOT EXISTS architecture_diffs_queue_idx
    ON architecture_diffs (status, lease_expires_at, created_at);
