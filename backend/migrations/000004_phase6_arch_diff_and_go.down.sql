DROP TABLE IF EXISTS architecture_diffs;

ALTER TABLE IF EXISTS import_references
    DROP COLUMN IF EXISTS evidence_metadata,
    DROP COLUMN IF EXISTS build_constraints,
    DROP COLUMN IF EXISTS column_number,
    DROP COLUMN IF EXISTS alias,
    DROP COLUMN IF EXISTS source_file;

ALTER TABLE IF EXISTS source_files
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS types_count,
    DROP COLUMN IF EXISTS methods_count,
    DROP COLUMN IF EXISTS files_count,
    DROP COLUMN IF EXISTS package_name,
    DROP COLUMN IF EXISTS directory_path,
    DROP COLUMN IF EXISTS package_import_path,
    DROP COLUMN IF EXISTS module_path,
    DROP COLUMN IF EXISTS language,
    DROP COLUMN IF EXISTS node_kind;

ALTER TABLE IF EXISTS analysis_runs
    DROP COLUMN IF EXISTS graph_granularity,
    DROP COLUMN IF EXISTS language;
