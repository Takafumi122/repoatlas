# RepoAtlas — アーキテクチャとディレクトリ構成 v1.0

## 1. 構成
```text
User
  │ GitHub repo URL
  ▼
Next.js web ──POST /api/v1/analyses──────► Go API (net/http)
  ▲                                            │
  │ GET status/graph                           │ repository metadata, commit SHA
  │                                            ▼
  └────────────────────────────────── PostgreSQL ◄── Go Worker
                                         ▲            │
                                         │            ├─ GitHub archive (HTTPS, allowlisted)
                                         │            ├─ ZIP guard (size/path)
                                         │            ├─ Python Tree-sitter parser
                                         │            ├─ Import resolver / graph builder
                                         │            └─ atomic result transaction
                                         └─ job queue (SKIP LOCKED)
```
- APIとWorkerは**別プロセス**。同一のGo modules/internal packagesを共有。
- キューはPostgreSQLのみ（Redis/Celery/KafkaはMVP非採用）。
- `analysis_runs`がスナップショット/状態/ジョブ両方の単位。解析結果は不変commit SHAに結びつく。
- リポジトリで得たコードを実行しない。ZIP中の通常ファイルを読み取り専用で解析する。

## 2. Go/Next.js monorepoのディレクトリ案
```text
repoatlas/
├── README.md
├── .gitignore
├── .env.example
├── Makefile
├── docker-compose.yml
├── .github/
│   └── workflows/
│       └── ci.yml
├── docs/
│   ├── 01-requirements.md
│   ├── 02-architecture.md
│   ├── 03-database.md
│   └── 04-api-examples.md            # 実装後
├── backend/
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   ├── api/main.go
│   │   └── worker/main.go
│   ├── internal/
│   │   ├── config/config.go
│   │   ├── httpapi/
│   │   │   ├── router.go
│   │   │   ├── handler.go
│   │   │   ├── types.go
│   │   │   └── middleware.go
│   │   ├── githubclient/
│   │   │   ├── client.go              # 実装を差し替えられるinterface
│   │   │   ├── rest.go
│   │   │   ├── archive.go
│   │   │   └── url.go                 # URLの厳密な検証
│   │   ├── analyzer/
│   │   │   ├── analyzer.go            # analyzer orchestration
│   │   │   ├── python/
│   │   │   │   ├── parser.go          # Tree-sitter構文木→imports/symbols
│   │   │   │   ├── module_index.go    # path→moduleインデックス
│   │   │   │   └── resolver.go        # import→内部ファイル
│   │   │   └── graph/
│   │   │       ├── graph.go           # edge deduplication
│   │   │       └── importance.go
│   │   ├── jobs/
│   │   │   ├── worker.go
│   │   │   ├── claim.go               # FOR UPDATE SKIP LOCKED
│   │   │   └── processor.go
│   │   ├── store/
│   │   │   ├── repository.go          # pgx SQL statements
│   │   │   ├── analyses.go
│   │   │   └── graphs.go
│   │   └── domain/
│   │       ├── analysis.go
│   │       ├── file.go
│   │       └── import.go
│   ├── migrations/
│   │   ├── 000001_init.up.sql
│   │   └── 000001_init.down.sql
│   └── testdata/
│       ├── python_small/             # ユニット解析fixture
│       ├── python_relative_imports/
│       └── archive_fixtures/         # malicious zipも含む生成fixture
├── frontend/
│   ├── package.json
│   ├── tsconfig.json
│   ├── next.config.ts
│   ├── app/
│   │   ├── layout.tsx
│   │   ├── page.tsx                   # URL入力
│   │   ├── analysis/[id]/page.tsx    # status/graph
│   │   └── globals.css
│   ├── components/
│   │   ├── RepoUrlForm.tsx
│   │   ├── AnalysisProgress.tsx
│   │   ├── RepositoryGraph.tsx
│   │   ├── FileNode.tsx
│   │   ├── FileDetailsPanel.tsx
│   │   └── GraphToolbar.tsx
│   ├── lib/
│   │   ├── api.ts
│   │   ├── types.ts
│   │   └── graph-layout.ts
│   └── __tests__/
│       └── RepositoryGraph.test.tsx
└── prompts/
    └── antigravity-phase1.md
```
**ルール**: 最初から空の大量ファイルを作る必要はない。必要になったときに対応するファイルを作る。`backend/internal`は責務の境界を優先し、抽象化を増やしすぎない。

## 3. 主要インターフェース
```go
// githubclient/client.go (conceptual; final shape can be adjusted)
type Client interface {
    Resolve(ctx context.Context, owner, repo string) (RepositorySnapshot, error)
    DownloadArchive(ctx context.Context, snapshot RepositorySnapshot) (io.ReadCloser, error)
}

type RepositorySnapshot struct {
    GitHubID int64
    Owner string
    Name string
    DefaultBranch string
    CommitSHA string
    HTMLURL string
}
```

```go
// analyzer/analyzer.go (conceptual)
type SourceFile struct {
    Path    string
    Content []byte
}
type Analyzer interface {
    Analyze(ctx context.Context, files []SourceFile) (GraphResult, error)
}
```
- API handlerはHTTP入出力のみ。GitHubアクセスやSQLを直接書かない。
- Worker processorはsnapshot, ZIP検査, parser, resolver, persistenceを順に呼ぶ。
- StoreとClientをinterfaceで差し替え可能にして外部接続無しのテストを可能にする。
- リポジトリルートと`src/`に対するモジュール名の衝突は一意でなければunresolved扱い。

## 4. 主要シーケンス
1. `POST /api/v1/analyses`でGitHub URLを構文検証。
2. APIが GitHubメタデータ+default branch先端commit SHAを取得。
3. `repositories`をGitHub IDでupsert。`analysis_runs`を `(repo_id, SHA, analyzer_version)` でinsert。既存なら既存IDを返す。
4. Workerが`queued`をclaimし`running`にしてleaseを設定。
5. archiveを取り、サイズ/パス検証。`.py`ファイルを抽出。
6. Tree-sitterで構文解析→モジュール索引→内部依存解決→重要度算出。
7. 1 transactionでfiles/imports/summaryを保存し`completed`へ。外部通信をトランザクション内に入れない。
8. Webが状態をpollし、完了後にgraphを取得。

## 5. 実装上の意思決定
- **D-01** GitHub URL→GitHub APIではhostが固定なので、ユーザー指定hostへサーバーからリクエストしない（SSRF防止）。
- **D-02** default branch名だけでは結果が変わるのでcommit SHAを取得。解析ごとの再現性を担保。
- **D-03** APIとWorkerは分離。時間のかかる解析がHTTP request timeoutを超えない設計。
- **D-04** 解析はTree-sitter公式Go bindings + Python grammar。CGOが必要ならDocker buildにC toolchainを含め、CGO_ENABLED=1で統一。バージョンはGo toolchainとの互換性を確認して固定。
- **D-05** ファイル間dependencyのみ。`from a import X`をPython実行時のシンボル解決と同等とは主張しない。
- **D-06** 実用上の描画上限はfrontendが制御する。backendの解析結果は勝手に欠落させない。
- **D-07** DBキューはMVPで1 worker、スケール時は`SKIP LOCKED`/lease処理をテストして拡張。

## 6. ローカル起動・コマンド（実装後の目標）
```bash
cp .env.example .env
# ローカルのDocker/Postgresに接続

docker compose up --build
# web: http://localhost:3000  api: http://localhost:8080

# 開発時の個別確認
cd backend && go test ./... && go vet ./...
cd frontend && npm ci && npm run lint && npm run typecheck && npm run test && npm run build
```
`.env.example`にはGITHUB_TOKEN（任意）、DATABASE_URL、NEXT_PUBLIC_API_BASE_URL、ALLOWED_ORIGINS、WORKER_CONCURRENCY、ANALYZER_VERSIONを記載し、秘密値をcommitしない。
