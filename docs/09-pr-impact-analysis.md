# RepoAtlas — Pull Request Impact Analysis (Phase 5 Task 1)

RepoAtlas Phase 5 introduces **Pull Request Impact Analysis**, transforming RepoAtlas from an exploratory visualization tool into a proactive code change impact analyzer for GitHub Pull Requests.

---

## 1. Overview & Architecture

When a user submits a public GitHub Pull Request URL (e.g., `https://github.com/owner/repo/pull/123`), RepoAtlas performs the following end-to-end pipeline:

```
[User Input: GitHub PR URL]
            │
            ▼
[URL Validation & SSRF Prevention]
    ├── Host allowlist (github.com only, no local/internal IP)
    ├── Strict regex (/owner/repo/pull/number)
    └── Path traversal rejection (no .. or url-encoded traversal)
            │
            ▼
[GitHub API Fetching & Commit Pinning]
    ├── GET /repos/{owner}/{repo}/pulls/{number} -> base.sha, head.sha, state, refs
    └── GET /repos/{owner}/{repo}/pulls/{number}/files (paginated, capped at 300 files)
            │
            ▼
[HEAD Dependency Graph Resolution]
    ├── Reuses existing RepoAtlas Tree-sitter static analyzer
    └── Automatic deduplication & caching on (repo_id, pr_number, base_sha, head_sha, analyzer_version)
            │
            ▼
[Multi-source Reverse BFS Impact Engine]
    ├── Roots: Added & Modified Python files in PR
    ├── Edge Direction: A imports B (B changed => A impacted)
    ├── Breadth-First Search on transposed dependency graph
    ├── Direct Impact (Depth 1) vs Indirect Impact (Depth >= 2)
    └── Deterministic shortest representative path calculation
            │
            ▼
[Interactive Graph & PR Impact Panel UI]
    ├── KPI Metrics: Changed Files, Direct Impact, Indirect Impact, Completeness
    ├── Changed Files list (badges for added/modified/removed/renamed)
    ├── Step-by-step representative dependency paths (A -> B -> C)
    └── Node click to focus and highlight in React Flow canvas
```

---

## 2. GitHub REST API Integration & Constraints

### 2.1 Endpoints Used
1. `GET /repos/{owner}/{repo}/pulls/{pull_number}`
   - Fetches PR title, state, base SHA/ref, head SHA/ref.
   - Pinned SHA validation: Fork PRs are supported because `head.sha` points to the exact commit in the head repository or fork.
2. `GET /repos/{owner}/{repo}/pulls/{pull_number}/files?per_page=100&page=N`
   - Iterates through paginated file lists.
   - **Safety Cap**: Capped at `MaxPRFiles = 300` files to protect memory and rate limits.
   - **Completeness Tracking**: If files exceed 300, `AnalysisCompleteness` is marked as `"partial"` rather than misleadingly reporting `"complete"`.

### 2.2 Rate Limit Handling & Commit Consistency
- If GitHub API returns HTTP 403 / 429 rate limit errors, the worker returns a structured domain error (`ErrCodeGitHubRateLimited`) with retry guidance.
- PR metadata and file lists verify that `head.sha` did not change mid-fetch. If a PR is updated while fetching, the inconsistent run is rejected.
- **Limitation**: GitHub does not provide an API to fetch PR file diffs pinned to an arbitrary historical commit of a closed/rebased PR; once a PR branch moves, only the current HEAD diff is returned.

---

## 3. Multi-source BFS Impact Analysis Algorithm

### 3.1 Edge Direction & Propagation
In RepoAtlas, a dependency edge `A -> B` denotes `A imports B`.
When file `B` is modified in a PR, any module that directly or transitively imports `B` may be affected.
Therefore, the impact analysis traverses the **transposed graph** (`B <- A`).

### 3.2 Algorithm Details (`AnalyzeMultiSourceImpact`)
1. **Initialize**: Collect all changed Python files (`status IN ('added', 'modified', 'renamed')` and `is_python = true`) into root set $S$.
2. **Reverse Adjacency List**: Build inverted edge lookup `reverseAdj[target] = [sources]`. To ensure deterministic output regardless of iteration order, edges and node lists are lexically sorted.
3. **Queue Initialization**: Push each root $r \in S$ to the BFS queue with distance 0 and representative path `[r]`.
4. **Traversal**:
   - For current file $u$ at distance $d$:
   - For each incoming dependent $v \in reverseAdj[u]$:
   - If $v \notin visited$:
     - $v.depth = d + 1$
     - $v.impact\_type = \text{"direct" if } d+1=1 \text{ else "indirect"}$
     - $v.representative\_path = \text{path}(u) + [v]$
     - Add $v$ to BFS queue.
   - If $v \in visited$ at same depth $d+1$:
     - Append root changed file to $v.source\_changed\_files$.
5. **Cycle & Termination Safety**:
   - Cyclic imports (`A imports B` and `B imports A`) are safely handled by the `visited` map and depth checks.
   - Complexity is strictly $O(V + E)$ where $V$ is the number of files and $E$ is the number of internal import edges.

---

## 4. Handling File Statuses: Added, Modified, Removed, Renamed

| PR Status | Analyzed in HEAD Graph? | Behavior & Explanation |
|---|---|---|
| `added` | Yes | Analyzed as a new node in the HEAD dependency graph. |
| `modified` | Yes | Analyzed in the HEAD dependency graph. Serves as root for reverse BFS. |
| `renamed` | Yes | Analyzed under `path` (new filename), with `previous_path` recorded in metadata. |
| `removed` | No | Removed files do not exist in HEAD; they are recorded as `unanalyzed` with reason `file_removed_in_pr`. |
| Non-Python | No | Markdown, YAML, JSON, C/C++ files are marked `non_python_file` and excluded from Python BFS. |

---

## 5. REST Endpoints

### 5.1 Create PR Analysis
- **`POST /api/v1/pull-requests/analyses`**
  - Request: `{"url": "https://github.com/pallets/itsdangerous/pull/352"}`
  - Response (202 Accepted / 200 OK cached):
    ```json
    {
      "pr_analysis_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
      "status": "queued",
      "cached": false
    }
    ```

### 5.2 Poll PR Analysis Status
- **`GET /api/v1/pull-requests/analyses/{id}`**
  - Returns current status (`queued`, `running`, `completed`, `failed`), stage (`resolving_metadata`, `analyzing_head`, `analyzing_impact`, `persisting`), and metadata.

### 5.3 Fetch PR Impact Analysis Result
- **`GET /api/v1/pull-requests/analyses/{id}/impact`**
  - Returns complete PR impact payload:
    - `summary`: counts of changed files, direct/indirect impacts, completeness.
    - `changed_files`: file list with diff statistics and analysis status.
    - `affected_files`: affected nodes with depth, impact type, and representative paths.
    - `unanalyzed_files`: unanalyzed files with explicit reasons.
