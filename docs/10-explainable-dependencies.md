# RepoAtlas — Explainable Dependency Graph (Phase 5 Task 2)

Phase 5 Task 2 upgrades RepoAtlas's dependency graph from an opaque connectivity visualization into an **explainable, verifiable static analysis system**.

Every edge displayed in the graph is backed by verifiable static import evidence extracted by the Tree-sitter AST parser, including 1-based Unicode character columns, line numbers, import kinds, resolution statuses, and human-readable resolution reasons.

---

## 1. Precision AST Parsing & Character Offsets

### 1.1 1-based Unicode Rune Offsets
Standard byte offsets produce inaccurate character positions for multi-byte UTF-8 source code (e.g., Japanese comments, emoji, mathematical symbols).
RepoAtlas converts Tree-sitter byte ranges into 1-based Unicode rune columns and line numbers:

```go
// Calculate 1-based rune column offset within line
lineBytes := sourceLines[point.Row]
runeCol := 1 + utf8.RuneCount(lineBytes[:point.Column])
```

### 1.2 Extracted AST Import Fields
For each `import <mod>` and `from <mod> import <item>` AST node:
- `SourcePath`: relative path to importing Python file.
- `ImportStatement`: exact raw source line (e.g. `from .signer import Signer`).
- `ModuleName`: extracted module identifier (e.g. `.signer` or `click.core`).
- `ImportKind`: `"absolute"` vs `"relative"`.
- `StartLine`, `StartColumn`, `EndLine`, `EndColumn`: 1-based rune boundaries.

---

## 2. Import Resolution Engine & Reason Catalog

When resolving an import to a target file within the repository, RepoAtlas assigns a classification and explicit human-readable resolution reason:

| Status | Classification | Resolution Reason Pattern |
|---|---|---|
| `internal` | Resolved | `"internal import resolved to file {path}"` |
| `internal` | Resolved (Relative) | `"relative import resolved to file {path} within package"` |
| `external` | Standard Library | `"standard library module '{name}'"` |
| `external` | Third-Party Package | `"third-party package '{name}' not located in repository source"` |
| `unresolved` | Ambiguous Module | `"ambiguous import: multiple candidate files match '{name}'"` |
| `unresolved` | Out of Bounds | `"relative import escapes repository root or package boundary"` |
| `unresolved` | Not Found | `"module '{name}' could not be located in repository or known libraries"` |

---

## 3. Database Persistence: `import_evidence`

```sql
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
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Database Constraints
- Foreign keys ensure `analysis_id`, `source_file_id`, and `target_file_id` belong to the same analysis run.
- Classification constraint enforces that `internal` imports have non-null `target_file_id`, while `external` and `unresolved` have null `target_file_id`.
- Composite indexes provide sub-millisecond querying by edge `(analysis_id, source_file_id, target_file_id)` and status `(analysis_id, resolution_status)`.

---

## 4. REST Evidence API

### `GET /api/v1/analyses/{id}/evidence`
Query parameters:
- `source_path` (optional): Filter by source file.
- `target_path` (optional): Filter by target file (returns edge evidence).
- `unresolved_only` (optional, boolean): If `true`, returns all unresolved imports.

Response:
```json
{
  "analysis_id": "018f972b-4235-7798-8e3b-9a8c081e6490",
  "source_path": "src/itsdangerous/url_safe.py",
  "target_path": "src/itsdangerous/signer.py",
  "count": 1,
  "evidences": [
    {
      "id": 42,
      "analysis_id": "018f972b-4235-7798-8e3b-9a8c081e6490",
      "source_path": "src/itsdangerous/url_safe.py",
      "target_path": "src/itsdangerous/signer.py",
      "import_statement": "from .signer import Signer",
      "module_name": ".signer",
      "start_line": 10,
      "start_column": 1,
      "end_line": 10,
      "end_column": 27,
      "import_kind": "relative",
      "resolution_status": "internal",
      "resolution_reason": "relative import resolved within same package",
      "github_url": "https://github.com/pallets/itsdangerous/blob/1234567/src/itsdangerous/url_safe.py#L10"
    }
  ]
}
```

---

## 5. UI Evidence Drawer & Unresolved Panel

1. **Edge Click Drawer (`EdgeEvidenceDrawer.tsx`)**:
   - Clicking any dependency edge in the graph opens a dedicated slide-over drawer displaying:
     - Edge source and target files.
     - AST import statement code block with syntax styling.
     - 1-based line numbers and rune column positions.
     - Direct clickable link to the exact line on GitHub (`#L10`).
2. **File Details Panel (`FileDetailsPanel.tsx`)**:
   - Displays all unresolved imports for the selected node with resolution status badges and human-readable explanation of why the import could not be resolved.
