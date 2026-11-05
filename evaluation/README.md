# RepoAtlas Evaluation Suite

This directory documents the evaluation methodology, datasets, and reproduction instructions for **RepoAtlas Phase 6 & Phase 6.5 (Go Static Analysis, Architecture Diff, and Performance Audit)**.

---

## 1. Quick Reproduction (1-Click Run)

The evaluation suite is implemented as a standalone CLI tool in `backend/cmd/eval`.

To run the complete benchmark and precision/recall evaluation across all synthetic scenarios and full cloned OSS repositories:

```bash
cd backend
go run ./cmd/eval
```

Sample output:

```text
================================================================================
RepoAtlas Phase 6 — Static Analysis & Architecture Diff Evaluation Suite
================================================================================

### 1. Go Static Analysis Evaluation (Task 6)

| Dataset / Target | Files | Packages | Internal Edges | Precision | Recall | F1 Score | Unresolved Rate | Latency (ms) | Memory (MB) |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| Scenario 1: Single-Module Microservice | 6 | 5 | 4 | 100.0% | 100.0% | 1.000 | 0.0% | 0.74 ms | 0.06 MB |
| Scenario 2: Multi-Module & go.work Replace | 5 | 2 | 1 | 100.0% | 100.0% | 1.000 | 0.0% | 0.08 ms | 0.02 MB |
| Scenario 3: Alias, Blank, Dot, Build Tags | 6 | 3 | 2 | 100.0% | 100.0% | 1.000 | 0.0% | 0.10 ms | 0.03 MB |
| Scenario 4: Out-of-Repo Replace (Unresolved Case) | 3 | 2 | 1 | 100.0% | 100.0% | 1.000 | 33.3% | 0.07 ms | 0.02 MB |
| Scenario 5: Ambiguous Module Collision (Unresolved Case) | 6 | 2 | 0 | 100.0% | 100.0% | 1.000 | 100.0% | 0.08 ms | 0.02 MB |
| Real Repo: spf13/cobra (v1.8.0 snapshot) | 5 | 2 | 1 | 100.0% | 100.0% | 1.000 | 0.0% | 0.17 ms | 0.04 MB |
| Real Repo: go-chi/chi (v5 snapshot) | 4 | 2 | 1 | 100.0% | 100.0% | 1.000 | 0.0% | 0.06 ms | 0.03 MB |
| Real Repo: jmoiron/sqlx (snapshot) | 3 | 2 | 1 | 100.0% | 100.0% | 1.000 | 0.0% | 0.04 ms | 0.02 MB |
| Full OSS: spf13/cobra (Full Repo, 36 files) | 36 | 2 | 1 | 100.0% | 100.0% | 1.000 | 0.0% | 6.88 ms | 5.42 MB |
| Full OSS: jmoiron/sqlx (Full Repo, 16 files) | 16 | 3 | 1 | 100.0% | 100.0% | 1.000 | 0.0% | 3.37 ms | 2.91 MB |
| Full OSS: go-chi/chi (Full Repo, 89 files, 3 modules) | 89 | 18 | 20 | 100.0% | 100.0% | 1.000 | 0.0% | 6.05 ms | 5.49 MB |

### 2. Architecture Diff & Tarjan SCC Scale Benchmark (Task 8)

| Node Scale | Edge Scale | Diff Latency | Tarjan SCC Latency | Memory (Alloc) | Added Cycles | Determinism | Complexity |
| :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| 50 nodes | 50 edges | 0.10 ms | 0.01 ms | 0.14 MB | 1 cycles | true (100%) | O(V + E) |
| 300 nodes | 300 edges | 0.35 ms | 0.06 ms | 0.86 MB | 1 cycles | true (100%) | O(V + E) |
| 1000 nodes | 1000 edges | 1.39 ms | 0.19 ms | 3.58 MB | 1 cycles | true (100%) | O(V + E) |
| 5000 nodes | 5000 edges | 7.22 ms | 1.17 ms | 18.37 MB | 1 cycles | true (100%) | O(V + E) |

================================================================================
Evaluation Complete: Zero-Execution verified, All benchmarks passed successfully.
================================================================================
```

---

## 2. Evaluation Datasets & Ground Truth Methodology

### 2.1 Independent Ground Truth Construction
To ensure zero bias and maintain test integrity:
- **Production Boundary**: RepoAtlas production code (`backend/internal/analyzer/golang`) runs under strict **Zero-Execution** rules: no compiler or runtime commands (`go run`, `go build`, `go test`, `go list`, `go mod download`) are ever invoked.
- **Evaluation Oracle**: Ground truth for real OSS repositories was generated independently using `go list -json ./...` strictly offline in test scripts.

### 2.2 Datasets
1. **Synthetic Scenarios (`backend/cmd/eval/main.go`)**:
   - **Scenario 1**: Single-Module microservice structure (`go.mod`, `cmd/server`, `internal/api`, `internal/service`, `internal/store`, `pkg/util`).
   - **Scenario 2**: Multi-Module repository with `go.work` and local `replace` directives.
   - **Scenario 3**: Advanced Go syntax constructs: blank imports (`_ "database/sql"`), alias imports (`svc "..."`), dot imports (`. "..."`), and build tags (`//go:build windows`).
   - **Scenario 4**: Out-of-repo replace directive (`replace example.com/external => ../outside/lib`). Verified that unresolved rate is 33.3% and no spurious internal edges are generated.
   - **Scenario 5**: Ambiguous module path collision (duplicate module path in distinct subtrees). Verified that unresolved rate is 100.0% to safely prevent false internal resolution.
2. **Real-World OSS Repositories (Full Clone Scans)**:
   - **`spf13/cobra`** (commit `adbc8813901bba65827259daa8e22ff94ec1f30e`): 66 total files, 36 Go files (19 non-test, 17 test), 2 packages (`cobra`, `doc`).
   - **`jmoiron/sqlx`** (commit `41dac167fdad5e3fd81d66cafba0951dc6823a30`): 25 total files, 16 Go files (9 non-test, 7 test), 3 packages.
   - **`go-chi/chi`** (commit `9f8adb1aabaa8a91180a821111d3f8e668d1c0bf`): 110 total files, 89 Go files (55 non-test, 34 test), 18 packages across 3 modules (`github.com/go-chi/chi/v5`, `versions`, `rest-example`).

---

## 3. Scale Benchmark Details

The benchmark dynamically generates dependency graphs of sizes **50, 300, 1,000, and 5,000 nodes**:
- Evaluates the $O(V + E)$ set comparison algorithm (`ComputeGraphDiff`).
- Evaluates Tarjan's Strongly Connected Components (SCC) algorithm for cyclic dependency detection.
- Tests determinism by computing SHA-256 output hashes over multiple consecutive runs.

See [`docs/15-phase6-evaluation.md`](../docs/15-phase6-evaluation.md) for full metrics, charts, and analysis.
