# Antigravity 指示: RepoAtlas Phase 1 — 動く縦割りMVPを実装する

あなたはシニアGoエンジニア兼フルスタックエンジニアです。現在のワークスペースに、GitHub公開Pythonリポジトリの依存グラフ可視化Webアプリ **RepoAtlas** を実装してください。

## 0. 絶対に最初にやること
1. ワークスペースを調査する。既存コード/README/設計ドキュメントがあれば読んで、壊さずに利用する。
2. ルートにある `docs/01-requirements.md`, `docs/02-architecture.md`, `docs/03-database.md`, `docs/04-schema.up.sql`, `docs/05-schema.down.sql` を**仕様の正本**として読む。未配置なら本プロンプトの記述を正本として扱う。
3. 実装計画を3〜6個のタスクに分けて順に実行する。各タスク完了後に必要なテストを実行し、エラーを修正する。計画だけで止まらないこと。
4. 追加の確認は、必須の機密情報がない場合を除き不要。アプリの形・UI・命名は仕様を優先して自律的に決める。

## 1. 今回の目標（Phase 1）
以下の一連の経路が**実装され、ローカルで動くこと**。

1. ブラウザでGitHub公開PythonリポジトリURLを入力。
2. Go APIがURL検証→GitHub repository metadata/default branch commit SHA解決→PostgreSQLに解析ジョブ登録。
3. Go Workerがジョブをclaimし、対象SHAのZIPを安全に取得してPython `.py`を抽出。
4. **Go + Tree-sitter Python grammar**でimport/関数/クラスを静的解析。内部ファイル依存を解決。
5. 解析結果をPostgreSQLに保存し、Go APIでノード/エッジをJSON返却。
6. Next.js + React Flowでファイル依存グラフを表示。ノードクリックで詳細を表示。
7. 実GitHub接続なしでも動作確認できるfixture-drivenテストおよびデモグラフを用意。

この最初のフェーズでは**最低限の実用UIでよい**。AI要約、GitHubログイン、private repo、Go/TSソース解析、関数call graph、分散queue、複雑な認可機能は実装しない。

## 2. 採用技術（勝手に変更しない）
- Backend: Go、stdlib `net/http`、pgx。Web/Workerは別エントリポイント `cmd/api`, `cmd/worker`。
- Python解析: `github.com/tree-sitter/go-tree-sitter` と `tree-sitter-python` のGo grammar。ライブラリの**実際の現在APIを確認してバージョン固定**。CGoが必要ならDockerを対応させる。Pythonの外部プロセスで構文解析する方式には置き換えない。
- Frontend: Next.js App Router / TypeScript / `@xyflow/react` / Dagre。
- DB: PostgreSQL、SQL migrationは `golang-migrate` または互換のmigration実行方法。
- Local: Docker Composeでpostgres/api/worker/webを起動。
- Tests: Go `testing`、フロント側はTypeScript typecheck / 必要に応じVitest。

## 3. 必須のディレクトリ構成
`docs/02-architecture.md`の提案を基本とする。`backend/internal/{httpapi,githubclient,analyzer/python,analyzer/graph,jobs,store,domain}`, `backend/cmd/{api,worker}`, `backend/migrations`, `backend/testdata`, `frontend/app`, `frontend/components`, `frontend/lib`, `.env.example`, `docker-compose.yml`, `README.md`は必要。

構造だけ作って中身の空ファイルを大量に増やすのは禁止。実際に必要なファイルから作る。

## 4. API契約
- `POST /api/v1/analyses` body `{"repository_url":"https://github.com/owner/repo"}`
  - 新規/処理中: 202 `{"analysis_id":"...","status":"queued|running","cached":false}`
  - 既存completed: 200 `{"analysis_id":"...","status":"completed","cached":true}`
- `GET /api/v1/analyses/{id}`: `id,status,stage,repository,commit_sha,error,created_at,completed_at`を返す。
- `GET /api/v1/analyses/{id}/graph`: completed時に`repository,commit_sha,summary,nodes,edges,recommended_file_ids,warnings`。
- `GET /api/v1/demo/graph`: 同梱fixture由来JSON。`demo:true`であること。
- `GET /healthz`: 200。
- APIエラー: `{"error":{"code":"...","message":"..."}}`。適切なHTTP statusを付与。

### Graph schema の基本
- node: `id="file:"+path`, `path,functions,classes,in_degree,out_degree,is_entrypoint,importance_score,github_url`
- edge: `source,target,kind="import"`
- `summary`: `files,internal_edges,external_imports,unresolved_imports`
- 同じsource→targetは一辺に重複排除。node/edgeは安定順にソート。
- 未完了のgraph取得は409。

## 5. Python静的解析仕様
- パス→module索引を作り、rootとトップレベル`src/`に対応。
- `.py`通常ファイルの`import`, `from ... import ...`、`function_definition`, `class_definition`を抽出。
- 相対import・`__init__.py`・`from pkg import submodule`の基本ケースを解決。
- **正しい依存だけをinternalにする。** 一意に解決できない場合はexternal/unresolvedとして記録し、虚偽のエッジを作らない。
- `importlib`など動的importはサポート対象外。
- 解析対象コードを実行しない。`python`/`pip`/リポジトリ内`Makefile`/`setup.py`/`git hooks`の実行は禁止。
- `importance_score`は仕様のヒューリスティック。上位5件をグラフJSONに載せる。ロジックは独立した関数にする。

## 6. GitHub API/セキュリティ
- 入力は`https://github.com/{owner}/{repo}`のみ（末尾`/`と`.git`は許容）。username/password, port, query, fragment, 追加path, github.com.evil.exampleなどは拒否。
- APIの接続先は`api.github.com`に固定。repoの`default_branch`先端をSHA解決した後、SHAのarchiveを取得する。固定したSHAを表示する。
- GitHub側URLは自前で構築し、不正なuser URLへGETしない。リダイレクトを許可する場合も**全hopをHTTPS+GitHub archive配信の許可ホストに制限**する。
- 公開リポジトリだけ解析。レスポンスがprivateなら拒否。トークンはサーバー環境変数`GITHUB_TOKEN`のみ、未設定でも公開repoの匿名取得を可能にする。
- ZIPの圧縮50MiB/展開合計100MiB/一つのソース1MiB/全Pythonソース合計20MiB/Pythonファイル2000件/解析90秒を制限。過大入力は明示エラー。Zip Slip、絶対パス、symlink、異常なpath、ZIP bombを防ぐ。ZIPエントリは原則ディスクに解凍しない。
- network timeout, context cancellation, API 403/429の扱いを実装。指数バックオフは上限回数あり、rate-limit解除まで無限待機しない。
- ログにトークンやファイル内容を記録しない。CORSは設定originのみ。
- 公開デモを想定し、少なくとも過剰な同時実行防止（初期worker concurrency 1）とリクエストサイズ上限を備える。公開時のIPレート制限はREADMEで未対応なら明記（Phase 2で実装）。

## 7. DBとworker
- `docs/04-schema.up.sql`を`backend/migrations/000001_init.up.sql`へ、downも同様に配置して実際にmigration適用。
- `repositories`: github_repository_id(unique), owner, name, default_branch, html_url。
- `analysis_runs`: repository_id/commit_sha/analyzer_versionの複合unique、status、stage、lease、attempt、error、summary。
- `source_files`: (analysis_id,path) unique、関数/クラス/degree/importance。
- `import_references`: source/nullable target、解析行、class=internal/external/unresolved。複合FKで同run内だけの依存にする。
- workerは`FOR UPDATE SKIP LOCKED`でclaimし、DBロックを解放してからネットワーク/解析。リース切れを回復し、試行回数上限を持たせる。
- parseが終わってから1トランザクションでsource_files/imports/集計/`completed`を書き込む。失敗時には`failed`、機密なしのerror_code/error_message。
- 再解析ではcommit SHA + parser_version一致ならキャッシュを返す。同時リクエストで二重解析しない。

## 8. フロントの最小UI
- 明るくすっきりした開発者ツール風のレイアウト。過度に装飾せず見栄えよく。
- ホームにURL入力・説明・「デモを開く」。
- `/analysis/[id]`にpending/running/failed/completedを表示。pollingは1〜2秒、完了後停止、unmount時abort/cleanup。
- completedでReact Flowにfile nodesとimport edges。Dagreで初期配置、ズーム/パン/fit view。
- ノードクリックでpath、関数数、クラス数、内部被依存数/依存先数、importance、GitHubへのソースリンクを表示。
- 300ノード超なら重要ファイル＋選択ディレクトリなどで表示を制限し、総件数と表示件数を明示。
- GitHubが失敗したときも`demo`ページでグラフを操作可能にする。
- ローディング/空グラフ/エラーのstateを作り、APIにない架空の解析結果を表示しない。

## 9. 必須テスト
1. URL検証: 正常URL、`.git`、末尾`/`、evil host、port、query/fragment、path traversal、不正owner。
2. Tree-sitter解析: import, `from`, alias, relative import, `__init__.py`, `src/`、関数/クラス数、循環依存、外部モジュール、未解決。
3. graph: 重複エッジが1本になる、self-edgeの取り扱い、importance安定順。
4. GitHub client: `httptest`等のモックで200/404/403/429/redirect異常とtimeout。実APIに依存するCIを作らない。
5. ZIP検証: Zip Slip、symlink、容量上限、ファイル件数上限。fixtureをテスト内で生成してもよい。
6. API: POST→GET status→GET graphのレスポンス形式。cache hit、未完了graphは409。
7. Frontend: typecheck/build、および重要UIの最低限のunit test。

## 10. Definition of Done（このフェーズの完了条件）
- `docker compose up --build`でPostgres/API/Worker/Webが起動する。
- `GET /healthz`は200。
- `GET /api/v1/demo/graph`で本当に描画できるノードとエッジが得られる。
- mock GitHub client + fixture archiveで`POST→queued/running→completed→graph`を結合テストできる。
- 可能ならネットワーク許可・GitHub API rate limit範囲内で小さな実公開repoのsmoke testを一度だけ実行。不可なら実施不能と報告し、**テストに成功したように書かない**。
- `go test ./...` `go vet ./...` `npm run lint` `npm run typecheck` `npm run build` が成功。
- `.env.example`とREADME（起動・設計・制約・将来拡張・テスト・CGO注意）が存在。
- 作っただけの未実装stubや、全test skip、ダミーの成功レスポンスで合格扱いにしない。

## 11. 実行ポリシー
- 作業はこのリポジトリのworkspaceに限定。workspace外削除、破壊的操作、機密読み出し、リモートへのpush/deployは行わない。
- 依存のインストール、Docker build、テスト、formatterなど必要な作業は自律的に進める。
- `.env`の本物のシークレット値を生成・commitしない。認証がなくてもfixtureテストを完了できる。
- 既存ユーザーのファイルを消したり上書きする前に確認。細かな実装判断は自律的に行う。
- 大きな作業では必要最小限の縦割りを優先し、動く単位で段階的にコミット可能な状態を作る。

## 12. 最終レポート形式（日本語）
最後に、以下を簡潔に報告する。
1. 実装した機能一覧
2. 動作確認方法（コマンドとURL）
3. 実行したテストと実際の結果（成功/失敗/未実行）
4. 意図的に未実装にしたもの/既知の制約
5. 変更した主要ファイルと次のPhase 2への提案

**計画だけで終了せず、今から実装を開始してください。**
