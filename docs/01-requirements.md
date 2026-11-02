# RepoAtlas — MVP 要件定義 v1.0

更新日: 2026-10-30
ステータス: 初期仕様（実装前）

## 1. プロダクト
**RepoAtlas — Explore any codebase visually.**

GitHubの公開PythonリポジトリのURLを入力すると、ソースを静的解析し、ファイル単位の依存グラフと読むべき重要ファイルを可視化するWebアプリケーション。初見のOSS・チームのコードベースを探索しやすくする。

### ターゲット
- OSSを読み始めたエンジニア・学生
- 初めて参加するプロジェクトの構造を理解したい開発者
- ポートフォリオを触って評価する採用担当者

### 価値仮説
- ディレクトリツリーだけよりも「どこから読めばいいか」を短時間で把握できる
- 依存の集中するファイルやエントリーポイントにすぐ移動できる
- インストール・GitHubログイン不要の公開デモとして試せる

### 非目標
- 実行時トレース/厳密な関数呼び出しグラフ
- コードの正しさ・脆弱性の判定
- importの100%完全解決（Pythonの動的性質上不可能）
- LLMによる意味理解/AI要約、private repo、マルチ言語

## 2. MVPの機能要件
| ID | 優先 | 機能 | 受け入れ条件 |
|---|---|---|---|
| F-01 | Must | リポジトリ入力 | `https://github.com/{owner}/{repo}` と末尾`/`/`.git`を受け付け、GitHub以外・不正パスを拒否。publicのみ。 |
| F-02 | Must | スナップショット固定 | GitHub REST APIでdefault branchのcommit SHAを解決し、そのSHAのZIPを取得。結果にSHAを表示。 |
| F-03 | Must | 非同期解析 | `POST /api/v1/analyses`が202とIDを返し、ジョブ状態・ステージをGETで取得。失敗理由を表示。 |
| F-04 | Must | Python解析 | Tree-sitterで`.py`のimport/from-import、関数、クラスを抽出。コードを実行しない。 |
| F-05 | Must | 内部依存解決 | 解析対象リポジトリ内でファイルに一意解決できたimportのみ内部エッジにする。外部/不明は別カウント。 |
| F-06 | Must | 依存グラフ | 各ノード=Pythonファイル、エッジ=静的import。ズーム・パン・ノード選択・フィルタ。 |
| F-07 | Must | ファイル詳細 | パス、関数/クラス数、被依存数・依存先数、GitHubソースへのリンクを表示。 |
| F-08 | Must | 優先閲覧候補 | 被依存数・エントリーポイント候補から重要ファイル上位5件を選出し根拠を表示。 |
| F-09 | Must | キャッシュ | 同一repo ID/commit SHA/analyzer versionはcompleted結果を再利用。二重の実行要求も同一ジョブへ収束。 |
| F-10 | Must | Demo | GitHub障害時にも確認できる同梱の小さなデモグラフを持つ。デモは実リポジトリ解析結果と区別して明示。 |
| F-11 | Should | UI表示制限 | 解析ファイル数と描画ファイル数を区別。300ノード超は上位候補・ディレクトリフィルタで表示し、非表示件数を示す。 |
| F-12 | Should | グラフの探索 | ファイル名/パスで検索し、選択ノードの直接の依存元・依存先を強調する。 |

## 3. 解析仕様
- 対象: 公開リポジトリのdefault branchの特定commit。Python `.py`ファイル。
- 依存: `import a`, `import a.b`, `from a import b`, `from . import b`, `from ..x import y` 等の**静的に記述された**import。
- モジュール探索: repository root と直下`src/`をソースルート候補として使う。`.py`とパッケージ`__init__.py`を索引化する。曖昧なときは無理に解決しない。
- 分類: `internal`（一意に解決したリポジトリ内ファイル）、`external`（内部索引に一致せず外部と思われる）、`unresolved`（相対解決不能・曖昧など）。`external`も実際の導入済みパッケージであることを保証しない。
- `from pkg import name`: `pkg.name`というファイル/パッケージが一意に存在すればそれを優先。なければ`pkg/__init__.py`への依存を採用可能。それでも不明ならunresolved/externalに分類。シンボル呼び出し関係にはしない。
- 動的な`importlib.import_module()`等は非対応として明記。
- 同一src→dstの複数importは**グラフ表示では1本のエッジ**。DBには元のimport位置を別々に保存可。
- 重要度 v1: `min(10, 2*in_degree + 0.5*out_degree + entrypoint_bonus)`。`entrypoint_bonus=3`、候補ファイル名 `main.py`, `app.py`, `__main__.py`, `cli.py`。重みはヒューリスティックで、絶対的な正しさや実行頻度を表さない。
- 推奨順: 重要度降順（同点はパス辞書順）上位5件。理由に被依存数・エントリーポイント判定を表示。
- 重要度や依存関係は**静的解析の推定**であり、正確性限界をUI上に注記する。

## 4. UXフロー
1. トップ: URL入力 + サンプルURL選択 + 「デモを開く」。
2. 入力直後: POSTを実行、`analysis_id`を受信。ローディング画面へ。
3. 解析中: 1〜2秒間隔で `GET /api/v1/analyses/{id}` をポーリング。ステージ `queued → fetching → indexing → parsing → resolving → persisting → completed`を表示（％はMVPでは表示しない）。
4. 完了: グラフ画面を表示。上部はrepo名・commit短縮SHA・解析ファイル数・内部エッジ数・未解決数。
5. ノード選択: 右側に詳細パネル、クリックで選択。GitHubソースを新規タブで開ける。
6. 失敗: 分かる日本語の説明と再試行導線。レート制限時は可能な範囲で解除時刻を示す。
7. 小画面: グラフを優先し詳細パネルをタブ/ドロワーに切り替える。

## 5. API契約 v1
**POST `/api/v1/analyses`** — リポジトリ解析要求。
Request:
```json
{"repository_url":"https://github.com/owner/repo"}
```
Response: `202 Accepted`（新規/処理中）、キャッシュ完了済みは`200 OK`。いずれも同一形の結果。
```json
{"analysis_id":"uuid","status":"queued","cached":false}
```

**GET `/api/v1/analyses/{id}`** — 状態取得。
```json
{
  "id":"uuid", "status":"running", "stage":"parsing",
  "repository":{"owner":"owner","name":"repo","html_url":"https://github.com/owner/repo"},
  "commit_sha":"40-character-sha",
  "error":{"code":null,"message":null},
  "created_at":"2026-10-30T00:00:00Z",
  "completed_at":null
}
```

**GET `/api/v1/analyses/{id}/graph`** — `completed`時に返す。未完了なら`409 ANALYSIS_NOT_READY`。
```json
{
  "analysis_id":"uuid",
  "repository":{"owner":"owner","name":"repo","html_url":"https://github.com/owner/repo"},
  "commit_sha":"40-character-sha",
  "summary":{"files":4,"internal_edges":3,"external_imports":2,"unresolved_imports":1},
  "nodes":[{"id":"file:app/main.py","path":"app/main.py","functions":1,"classes":0,"in_degree":0,"out_degree":2,"is_entrypoint":true,"importance_score":4,"github_url":"https://github.com/owner/repo/blob/<sha>/app/main.py"}],
  "edges":[{"source":"file:app/main.py","target":"file:app/service.py","kind":"import"}],
  "recommended_file_ids":["file:app/main.py"],
  "warnings":["Static import graph; dynamic imports are not included."]
}
```
ノードIDは解析単位で安定した `file:` + 正規化された相対パス。バックエンドは**全解析ノード**を返す（上限内）、フロントのみ描画数を制限。返す順はpath昇順/edgeのsource,target昇順で固定。

**GET `/api/v1/demo/graph`** — デモ用に同梱したfixtureの静的JSONを返す。必ず`demo:true`を付ける。

**GET `/healthz`** — APIプロセス生存確認。

エラー共通:
```json
{"error":{"code":"INVALID_REPOSITORY_URL","message":"GitHubの公開リポジトリURLを指定してください"}}
```
エラー例: `INVALID_REPOSITORY_URL(400)`、`REPOSITORY_NOT_FOUND(404)`、`REPOSITORY_NOT_PUBLIC(422)`、`REPO_TOO_LARGE(413)`、`GITHUB_RATE_LIMITED(429)`、`ANALYSIS_NOT_READY(409)`、`INTERNAL_ERROR(500)`。requestが受理済みのワーカー内失敗は解析ステータスを`failed`にする。

## 6. 非機能・セキュリティ
- URL入力は厳密に`https://github.com/{owner}/{repo}`。username/password、port、query、fragment、追加パスは拒否。`.git`末尾は許可し取り除く。owner/repoを保守的な正規表現で検証。
- HTTPリクエスト先はアプリが組み立てた`api.github.com`および許可済みGitHub archive配信先のみ。入力URLへ直接GETしない。**各リダイレクト先のホスト・HTTPSを再検証**。想定外ホストへの転送は拒否。
- メタデータAPIでprivateを拒否。秘密鍵はサーバー環境変数のみ、フロントに出さない。
- ZIPはバイトサイズ上限付きでストリーミング受信。目安: 圧縮50MiB、ZIP内総展開サイズ100MiB、個々のソース1MiB、Pythonファイル数2,000、合計ソース20MiB、解析時間90秒。超過時は明示的エラー。
- ZIPのパス正規化、`../`/絶対パス/シンボリックリンク拒否、ZIP bomb対策。原則ZIPをディスクに展開せずエントリをメモリ上で読む。読むのは `.py` の通常ファイルのみ。
- **解析対象のコードを実行しない**、`pip install`・`go run`・`git clone`・リポジトリ内スクリプト実行もしない。
- Worker同時実行1（初期値）、ジョブリースと期限切れ回復、SQLトランザクションで結果を原子的に保存。
- デモ公開時はアプリ単位のIPレート制限例: 新規解析10回/時/IP（分散デプロイなら共有ストアを利用）、進行中ジョブ数制限、リクエストタイムアウトを設定。ローカル環境では緩和可。
- GitHub API匿名60件/時/IPが基本上限。開発・公開運用はサーバー側トークンを任意設定可能。403/429なら`Retry-After`/rate-limit情報に従い、無限リトライしない。
- ログにアクセストークンやソースコード本文を出さない。機密やprivate repoに対応しない。
- CORSは設定したフロントエンドoriginに限定。入力をそのままHTMLに埋め込まない。

## 7. テストとリリース条件（Definition of Done）
- `go test ./...`、`go vet ./...`、`go test -race ./...`（対応環境）成功。
- フロントのlint・typecheck・unit test・build成功。
- `docker compose up --build`後にDB migration/API/worker/webが起動する。
- GitHubアクセスをモックしたfixtureテストで `import a`, `from pkg import x`, relative imports, `__init__.py`, `src/`、循環依存、unresolvedをカバー。
- 無効URL/非公開/404/429/巨大ZIP/Zip Slip/圧縮爆弾について期待通りのエラー。実際のGitHubレート制限を起こすテストは禁止。
- 新規解析→完了→graph表示のE2E成功。再解析時キャッシュを返すこと。
- GitHubトークン未設定・テストfixtureでもデモ操作が成立する。
- READMEに画像、URL（デプロイ後）、セットアップ手順、技術選定、構成、制約、テスト方法を記載する。

## 8. 段階的な実装順
- **Phase 1（最初のプロンプト）:** scaffold + migration + Go API/worker + fixture-driven Python parser/resolver + minimal React Flow UI。GitHub fetchは本実装またはAPIをmock可能なinterfaceで実装。`URL → 202 → completed → graph`の縦割りを目標とする。
- **Phase 2:** Security hardening + ZIP制限/リダイレクト制御 + fixture網羅・GitHub接続テスト。
- **Phase 3:** 検索・依存ハイライト・重要度/推薦・モバイルUI + 公開デプロイ。
- **Phase 4（将来）:** Go/TypeScript解析、差分比較、重要ファイルの説明、モジュールクラスタリング。

## 9. 技術一次資料
- GitHub REST API: https://docs.github.com/en/rest/repos/repos
- GitHub archive download: https://docs.github.com/en/rest/repos/contents
- GitHub REST API rate limit: https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api
- Go tree-sitter: https://github.com/tree-sitter/go-tree-sitter
- React Flow: https://reactflow.dev/learn/concepts/building-a-flow
