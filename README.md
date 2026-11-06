# RepoAtlas — Zero-Execution Multi-Language (Python & Go) Architecture & Diff Explorer

<div align="center">

[![Live Demo](https://img.shields.io/badge/Live_Demo-Try_Online-success?style=for-the-badge&logo=railway&logoColor=white)](https://web-production-6e4cf.up.railway.app/)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg?style=for-the-badge)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=for-the-badge&logo=go&logoColor=white)](backend/go.mod)
[![Next.js](https://img.shields.io/badge/Next.js-15-black?style=for-the-badge&logo=next.js&logoColor=white)](frontend/package.json)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=for-the-badge&logo=postgresql&logoColor=white)](docs/03-database.md)

### URL 👉 **https://web-production-6e4cf.up.railway.app/**

</div>

---

**RepoAtlas** は、公開 GitHub リポジトリ（**Python & Go**）の URL を入力するだけで、**リポジトリ内のコードを一切実行せずに静的解析**し、依存関係グラフ・最初に読むべき推奨順序（**Guided Code Tour**）・変更時の潜在的影響範囲（**BFS Impact Analysis**）・ディレクトリ単位のアーキテクチャ構造（**Architecture View**）・そして2つのコミット間のアーキテクチャ変化を可視化する（**Architecture Diff**）ブラウザ上でインタラクティブに探索できる Web アプリケーションです。

---

## 1. プロダクト概要と解決したい課題

### 解決したい課題
初見の OSS やチームの大規模な Python / Go リポジトリを読み始める際、開発者はしばしば以下の課題に直面します。

1. **「どのファイル・パッケージから読めばよいか分からない」**：ディレクトリツリーを上から順に開いても、設定ファイルやユーティリティばかりが目に入り、実際のエントリーポイントや主要な制御フローになかなか辿り着けない。
2. **「このパッケージやファイルを変更したらどこに波及するか分からない」**：修正対象を直接 `import` している箇所だけでなく、2 ホップ・3 ホップ先の間接的な依存元を手作業の grep で追跡するのは困難。
3. **「2つのコミットやPRでアーキテクチャがどう変わったか分からない」**：行単位の差分（Git Diff）では、モジュール間の循環依存（Cycle）の混入や不要な結合の増加を見落としやすい。
4. **「不審・未知のリポジトリをローカルで実行したくない」**：動的解析ツールや `go run` / `setup.py` / `pip install` を伴うツールは任意コード実行のリスクがある。

### RepoAtlas のアプローチ
RepoAtlas は **「ゼロ実行（Zero-Execution）」** と **「決定論的グラフアルゴリズム」** を組み合わせ、対象コミット SHA の ZIP アーカイブをインメモリ制限下で安全に展開。Python は Tree-sitter、Go は Go標準パーサ（`go/parser`, `go/ast`）およびモジュールパーサ（`modfile`）による構文解析と $O(V + E)$ グラフ探索（Tarjan SCC / BFS）によって、数秒でコードベースの地図と差分を生成します。

---

## 2. 主要機能

- **Go言語 & Python マルチ言語サポート (Go & Python Support)**
  - Python はファイル単位（`file:<path>`）、Go は Package単位（`pkg:<import_path>`）のグラフ粒度をサポート。
  - Go の `go.mod`、`go.work`、マルチモジュール、ローカル `replace`、`internal/`、`package main`、ビルドタグ（`//go:build`）を完全網羅。
  - 同一パッケージ内の複数 Go ファイルの定義を集約しつつ、依存の根拠となる全 import 文と GitHub 行番号への証跡（**Explainable Dependencies**）を保持。
- **Architecture Diff（2コミット間のアーキテクチャ差分比較）**
  - BASE と HEAD のコミット SHA を指定し、ノードの追加（緑）、削除（赤）、変更（黄）、不変（スレート）およびエッジ差分を可視化。
  - **Tarjan SCC アルゴリズム**により、HEAD で新しく発生した循環依存（Added Cycles ⚠️）や解消された循環依存（Resolved Cycles ✅）を線形時間 $O(V + E)$ で検知。
  - Union Graph に対する統一 Dagre レイアウトにより、**Diff / BASE / HEAD 表示を切り替えてもノードの座標がジャンプしない安定レイアウト**を実現。
- **Guided Code Tour（推奨閲覧順序の自動生成）**
  - 単なる被依存数ランキングではなく、**「Entrypoint / 公開 API 境界 → 主要制御フロー（Orchestrator） → 中核モデル・共有基盤（Core / Foundation）」** の 3 段階ヒューリスティックで 5〜10 件の推奨閲覧順序と理由を決定論的に生成。
- **Impact Analysis（BFS による変更影響範囲解析）**
  - 任意のファイル/パッケージを選択すると、逆方向 `import` エッジを幅優先探索（BFS）して直接依存元（Depth 1）と間接依存元（Depth 2+）を算出。循環依存に対する無限ループ防止と最大探索深度の切り替え、キャンバス上の影響経路ハイライトを備えています。
- **Architecture View（ディレクトリ集約グラフ）**
  - ファイル/パッケージ単位のビューとディレクトリ単位に集約した `Architecture View` をワンクリックで切り替え可能。
- **Pull Request Impact Analysis & Explainable Dependencies（PR影響範囲解析 & 依存根拠）**
  - GitHub Pull Request URL（`https://github.com/owner/repo/pull/123`）を入力し、BASE/HEAD コミットを特定して変更ファイル起点での Multi-source BFS 逆方向影響解析を実行。
  - import文の Unicode 正確な行・列番号、構文、解決理由（internal / external / unresolved）、GitHub パーマリンクを含む証跡（**Explainable Dependency Graph**）を表示。
- **正確なコミットスナップショット & キャッシュ**
  - 解析時点のコミット SHA（40 文字）を固定し、各ノードから GitHub ソースへ直接ジャンプ可能。同一 `(repository_id, commit_sha, analyzer_version)` の再リクエスト時は PostgreSQL キャッシュから即座に返却します。

---

## 3. スクリーンショット

| トップ画面（1440px デスクトップ） | 依存グラフ & Guided Code Tour（File View） |
| --- | --- |
| ![Home Screen](docs/images/home-1440.png) | ![Graph File View](docs/images/graph-file-view-1440.png) |

| Architecture View（ディレクトリ集約） | Architecture Diff 入力画面 |
| --- | --- |
| ![Architecture View](docs/images/graph-arch-view-1440.png) | ![Architecture Diff Form](docs/images/diff-form-1440.png) |

---

## 4. アーキテクチャ図

```mermaid
flowchart TB
    subgraph Client["Browser (Next.js 15 App Router / React Flow / Dagre)"]
        UI["RepoUrlForm / GuidedTourBar / RepositoryGraph / ArchitectureDiffCanvas"]
    end

    subgraph Backend["Go Backend Services (net/http + pgx/v5)"]
        API["Go HTTP API (:8080)<br/>Rate Limiter / Trusted Proxy IP / Structured Logger"]
        Worker["Go Async Worker<br/>Lease Heartbeat + Fencing + Graceful Shutdown"]
        Composite["Composite Analyzer"]
        PythonParser["Tree-sitter Python Analyzer (CGO)"]
        GoParser["Go Standard AST & Module Analyzer (Zero-Execution)"]
        DiffEngine["Architecture Diff & Tarjan SCC Engine O(V+E)"]
        GraphEngine["Graph Exploration Engine<br/>Guided Tour + BFS Impact + Architecture View"]
    end

    subgraph Storage["Persistence & Queue"]
        PG[("PostgreSQL 16<br/>repositories / analysis_runs / source_files / import_references / architecture_diffs")]
    end

    subgraph External["External (Allowlisted HTTPS Only)"]
        GH["GitHub REST API & Codeload ZIP Archive<br/>api.github.com / codeload.github.com"]
    end

    UI -- "POST /api/v1/analyses<br/>POST /api/v1/architecture-diffs<br/>GET /api/v1/architecture-diffs/{id}/graph<br/>GET /api/v1/analyses/{id}/evidence" --> API
    API -- "1. Resolve Repo & Commit SHA" --> GH
    API -- "2. Upsert & Enqueue (Advisory Lock Queue Cap)" --> PG
    Worker -- "3. Claim Job (FOR UPDATE SKIP LOCKED)<br/>+ Periodic Lease Heartbeat" --> PG
    Worker -- "4. Fetch Commit ZIP (In-Memory Sanitizer)" --> GH
    Worker --> Composite
    Composite --> PythonParser
    Composite --> GoParser
    Composite --> GraphEngine
    API --> DiffEngine
    GraphEngine -- "5. Atomic Transaction Save<br/>(Fenced by lease_owner)" --> PG
```

---

## 5. 技術スタック

| レイヤー | 技術 / ライブラリ | 採用理由 |
| :--- | :--- | :--- |
| **Frontend** | Next.js 15 (App Router), React 19, TypeScript | 型安全な UI 構築と SSR / Static ルーティングの両立 |
| **Graph Visualization** | `@xyflow/react` (React Flow v12), `@dagrejs/dagre` | カスタムノード描画、階層型 DAG 自動レイアウト、ズーム・ミニマップ制御、Union Graph安定座標 |
| **Backend API & Worker** | Go 1.24 (`net/http`, `log/slog`), `jackc/pgx/v5` | 単一バイナリ展開、低メモリフットプリント、高並行性、標準ライブラリ中心の堅牢な HTTP 実装 |
| **Go Static Analysis** | `go/parser`, `go/ast`, `go/token`, `golang.org/x/mod/modfile` | Pure Go（CGO不要）、最新Go構文完全追従、ゼロ実行、Build Tagsメタデータ抽出 |
| **Python Static Analysis** | `tree-sitter/go-tree-sitter`, `tree-sitter-python` (CGO) | Python ランタイム不要で構文エラー耐性の高い高速 CST パース |
| **Diff & Cycle Engine** | Tarjan's SCC Algorithm, Deterministic Graph Diff | 線形時間 $O(V + E)$ での循環成分検知、ノード・エッジ変更の完全決定論的比較 |
| **Database & Queue** | PostgreSQL 16 | RDB トランザクションと `FOR UPDATE SKIP LOCKED` による外部ブローカー不要の信頼性の高いジョブキュー |
| **Testing** | Go `testing` (`-race`), `embedded-postgres`, Vitest, Playwright | ユニット・実 PostgreSQL 統合・ブラウザ E2E までの全層自動検証 |

---

## 6. ベンチマーク & 精度評価結果 (Phase 6 / 6.5 監査済み実測値)

### 6.1 Go言語静的解析の精度 (Task 6 / Phase 6.5 監査)
`backend/cmd/eval` による独立検証用正解（Ground Truth: オフラインの `go list -json ./...` 出力）との突き合わせ評価：

| データセット / 対象 | 全ファイル数 | 対象Goファイル数 | Package数 | 内部依存エッジ | Precision | Recall | F1 Score | 未解決率 | 解析時間 (ms) | メモリ割当 (MB) |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **Scenario 1: Single-Module** | 6 | 6 | 5 | 4 | **100.0%** | **100.0%** | **1.000** | 0.0% | 0.74 ms | 0.06 MB |
| **Scenario 2: Multi-Module (go.work)** | 5 | 5 | 2 | 1 | **100.0%** | **100.0%** | **1.000** | 0.0% | 0.08 ms | 0.02 MB |
| **Scenario 3: Complex / Build Tags** | 6 | 6 | 3 | 2 | **100.0%** | **100.0%** | **1.000** | 0.0% | 0.10 ms | 0.03 MB |
| **Scenario 4: Out-of-Repo Replace** | 3 | 3 | 2 | 1 | **100.0%** | **100.0%** | **1.000** | **33.3%** | 0.07 ms | 0.02 MB |
| **Scenario 5: Ambiguous Collision** | 6 | 6 | 2 | 0 | **100.0%** | **100.0%** | **1.000** | **100.0%** | 0.08 ms | 0.02 MB |
| **Full OSS: spf13/cobra** | 66 | 36 (非テスト19/テスト17) | 2 | 1 | **100.0%** | **100.0%** | **1.000** | 0.0% | 6.88 ms | 5.42 MB |
| **Full OSS: jmoiron/sqlx** | 25 | 16 (非テスト9/テスト7) | 3 | 1 | **100.0%** | **100.0%** | **1.000** | 0.0% | 3.37 ms | 2.91 MB |
| **Full OSS: go-chi/chi (3 modules)** | 110 | 89 (非テスト55/テスト34) | 18 | 20 | **100.0%** | **100.0%** | **1.000** | 0.0% | 6.05 ms | 5.49 MB |

> **精度・境界に関する監査注記**:
> - **Zero-Executionの厳守**: プロダクション環境（APIおよびWorker）では `go run`, `go build`, `go list`, `go mod download` 等の外部コマンドは一切実行されず、完全な静的構文解析のみで動作します（正解ラベル作成用の `go list` は評価ハーネスでのみ使用）。
> - **未解決率の明示**: Scenario 4（リポジトリ外へのreplace）やScenario 5（重複モジュール名衝突）のように、安全に解決できないケースは無理に100%解決と偽らず `unresolved` として安全側に倒しています。
> - **Go解析の制約**: `import "C"`（CGO）は外部依存として分類、リフレクションによる動的読み込みは静的解析の原理上対象外、ビルドタグ（`//go:build`）は排他的実行構成を保証するものではなく静的に確認可能な依存のスーパーセットとして収集します。

### 6.2 Architecture Diff & Tarjan SCC スケール評価 (Task 8 実測値)

| ノード規模 | エッジ規模 | Diff計算時間 (ms) | Tarjan SCC時間 (ms) | メモリ割当 (MB) | 検知循環数 | 決定論性 (Determinism) | 理論計算量 |
| :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **50 nodes** | 50 edges | 0.10 ms | 0.01 ms | 0.14 MB | 1 cycle | **Pass (100%)** | $O(V + E)$ |
| **300 nodes** | 300 edges | 0.35 ms | 0.06 ms | 0.86 MB | 1 cycle | **Pass (100%)** | $O(V + E)$ |
| **1,000 nodes** | 1,000 edges | 1.39 ms | 0.19 ms | 3.58 MB | 1 cycle | **Pass (100%)** | $O(V + E)$ |
| **5,000 nodes** | 5,000 edges | 7.22 ms | 1.17 ms | 18.37 MB | 1 cycle | **Pass (100%)** | $O(V + E)$ |

### 6.3 エンドツーエンド・パフォーマンス実測値 (Task 4)

測定範囲を各処理ステージごとに明確に区別した実測値です：

| ステージ | 測定対象 | 実測時間 / メトリクス | 測定環境・備考 |
| :--- | :--- | :--- | :--- |
| **1. 実Goリポジトリ静的解析** | ASTパース + ModIndex + 依存解決 | **3.37 ms 〜 6.88 ms** | 実OSS全走査 (Chi: 89ファイル / 18パッケージ) |
| **2. GitHub取得〜解析完了** | ZIP取得 + メモリ内展開 + 静的解析 | **10 ms 〜 25 ms** (テストFixture)<br/>**500 ms 〜 1,500 ms** (ライブGitHub API) | ネットワークRTTおよびGitHub Codeloadアーカイブ取得時間を含む |
| **3. DB保存時間** | PostgreSQL 16 `CompleteAnalysis` ACIDトランザクション | **19.76 ms** (189 ops, 50 packages/50 edges) | PostgreSQL 16 Alpine (Docker) |
| **4. Graph Diff計算** | BASE/HEADノード・エッジ差分 + Tarjan SCC | **0.10 ms** (50 nodes) 〜 **7.22 ms** (5,000 nodes) | 純粋なインメモリグラフ差分計算 |
| **5. ブラウザ初回レイアウト** | React Flow初期レンダリング + Dagre階層計算 | **58.99 ms** (50 nodes)<br/>**58.93 ms** (300 nodes)<br/>**8.55 ms** (1,000 nodes, 300上限キャップ) | Next.js 15 + React Flow v12 + Dagre |
| **6. ブラウザインタラクティブ操作** | ノード選択・BFS影響範囲ハイライト | **0.06 ms 〜 0.24 ms** | Dagre再計算なしのO(V)クラス名・属性更新 |
| **7. Architecture Viewレイアウト** | ディレクトリ集約レイアウト | **3.69 ms** (5 dirs) 〜 **29.12 ms** (40 dirs) | 集約ノード・エッジのDagreレイアウト |
| **8. プロセス常駐メモリ** | Worker / API常駐メモリ | **解析時ヒープ割当 2.9MB〜5.5MB** | Go 1.24 ランタイム GC |

---

## 7. ローカル起動方法

### 方法 A：Docker Compose で一括起動（推奨）

```bash
cp .env.example .env
docker compose up --build
```

- **Web UI**: `http://localhost:3000`
- **Architecture Diff 画面**: `http://localhost:3000/diff`
- **同梱デモ画面（通信不要）**: `http://localhost:3000/demo`
- **Go API Health Check**: `http://localhost:8080/healthz`

### 方法 B：ホスト環境で個別プロセスとして起動

```bash
# 1. Backend API の起動 (DATABASE_URL=memory 指定時はインメモリストア + 内蔵Workerで単体動作可能)
cd backend
DATABASE_URL=memory go run ./cmd/api

# 2. Frontend の起動
cd ../frontend
npm install
npm run dev
```

---

## 8. テスト・評価実行コマンド

```bash
# 1. Go 静的解析 & 全ユニットテスト (Race Detector 有効)
go -C backend vet ./...
go -C backend test -race -p 1 ./...

# 2. 静的解析精度・スケールベンチマークスイートの実行
go -C backend run ./cmd/evaluate # Python 静的解析評価スイート (17プロジェクト, 14 synthetic + 3 partial OSS)
go -C backend run ./cmd/eval     # Go 静的解析 & Architecture Diff 評価スイート (Tarjan SCC & OSS)

# 3. 実 PostgreSQL 16 統合テスト (Migration 000001〜000004 & Phase 6.5 全10項目E2E)
TEST_DATABASE_URL="postgres://repoatlas:repoatlas@localhost:5432/repoatlas?sslmode=disable" go -C backend test -v -run "TestPostgresIntegration|TestE2E" ./internal/...

# 4. Frontend 型チェック・ユニットテスト (Vitest 20テスト)・Production Build
npm --prefix frontend run lint
npm --prefix frontend run typecheck
npm --prefix frontend run test
npm --prefix frontend run build

# 5. Playwright ブラウザ E2E テスト
npm --prefix frontend run test:e2e
```

---

## 9. ライセンス

本プロジェクトは [MIT License](LICENSE) のもとで公開されています。
