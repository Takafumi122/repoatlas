# RepoAtlas — PostgreSQL DB設計 v1.0

## 1. ER概要
```text
repositories (1) ────── (N) analysis_runs
                                 │
                                 ├──── (N) source_files
                                 │              │
                                 │              └──── (N) import_references (source_file_id)
                                 │                               │
                                 └───────────────────────────────┘ (target_file_id, nullable)
```

**設計の要点**
- repoはGitHub repository IDで識別。owner/nameは変更されても同一repoをupsertできる。
- 解析結果は `(repository_id, commit_sha, analyzer_version)`でユニーク。default branchの進行とは独立。
- `source_files`は1 runに属する。比較時には別runのファイルと混線しない。
- `import_references`はimport原文・行・分類・解決先を保存。グラフのエッジは`internal`行を`DISTINCT`で集約し重複しない。
- `status`は `queued`,`running`,`completed`,`failed`。具体的な作業段階は`stage`で保持。

## 2. テーブル

### repositories
| column | type | 意味 |
|---|---|---|
| id | bigint PK | 内部ID |
| github_repository_id | bigint unique | GitHubが割り当てる安定ID |
| owner | text | 最新のowner |
| name | text | 最新のname |
| default_branch | text | 解析要求時点の表示用情報 |
| html_url | text | GitHub URL |
| created_at / updated_at | timestamptz | 管理日時 |

### analysis_runs
| column | type | 意味 |
|---|---|---|
| id | uuid PK | 外部公開するanalysis_id |
| repository_id | bigint FK | repositories.id |
| commit_sha | char(40) | 入力の固定コミット |
| analyzer_version | text | インポート解決・スコアリングのバージョン |
| status | text check | queued/running/completed/failed |
| stage | text | queued/fetching/indexing/parsing/resolving/persisting/completed/failed |
| attempt_count | integer | claim回数 |
| lease_expires_at | timestamptz nullable | workerのlease |
| error_code / error_message | text nullable | 失敗理由（機密情報なし） |
| files_count / internal_edges_count / external_imports_count / unresolved_imports_count | integer | 表示用の確定集計 |
| created_at / started_at / completed_at | timestamptz | 状態履歴 |

### source_files
| column | type | 意味 |
|---|---|---|
| id | bigint PK | 内部ID |
| analysis_id | uuid FK | 所属解析 |
| path | text | repo rootからの正規化相対POSIX path |
| functions_count / classes_count | integer | 静的に検出した数 |
| in_degree / out_degree | integer | ファイル間ユニークな依存辺数 |
| is_entrypoint | boolean | エントリーポイント候補 |
| importance_score | double precision | ヒューリスティック値 |

### import_references
| column | type | 意味 |
|---|---|---|
| id | bigint PK | 内部ID |
| analysis_id | uuid FK | 所属解析（複合FKで混在防止） |
| source_file_id | bigint FK | importを書いたfile |
| target_file_id | bigint nullable FK | internalなら必須 |
| import_text | text | 元のimport文字列の切り出し（長さ制限） |
| module_name | text | 解析したmodule名 |
| line_number | integer | 1始まり |
| classification | text check | internal/external/unresolved |
| resolution_note | text nullable | 未解決の理由/方針 |

## 3. SQLマイグレーション
実際のDDLは同梱の `04-schema.up.sql` / `05-schema.down.sql` に置く。実装時は `backend/migrations/000001_init.up.sql` と `.down.sql` にコピーして利用。

- `source_files(analysis_id, path)`はUNIQUE。
- `source_files(analysis_id, id)`もUNIQUEとして、`import_references`の複合FKで異なる解析runのファイルを誤参照できないようにする。
- `import_references`のsource/targetを同じ`analysis_id`に制約する。
- `analysis_runs(repository_id, commit_sha, analyzer_version)`をUNIQUEにしてキャッシュ重複を防ぐ。
- `analysis_runs(status, lease_expires_at, created_at)`にキュー用INDEX。

## 4. workerのジョブclaim
**PostgreSQL内のジョブを重複claimしない**ためにトランザクションで次の考え方を採用する。

```sql
WITH candidate AS (
  SELECT id
  FROM analysis_runs
  WHERE attempt_count < 3
    AND (status = 'queued'
     OR (status = 'running' AND lease_expires_at < now()))
  ORDER BY created_at
  FOR UPDATE SKIP LOCKED
  LIMIT 1
)
UPDATE analysis_runs a
SET status = 'running',
    stage = 'fetching',
    attempt_count = attempt_count + 1,
    started_at = COALESCE(started_at, now()),
    lease_expires_at = now() + interval '120 seconds'
FROM candidate
WHERE a.id = candidate.id
  AND a.attempt_count < 3
RETURNING a.id;
```
実運用では`attempt_count >=3`のリース切れ行を`failed`に遷移させる処理も必要。90秒以内の解析を前提に120秒leaseとし、超える可能性があればheartbeat更新を追加する。workerのトランザクションはclaim直後に終了させる（GitHub取得中にDBロックを持たない）。

### キャッシュ
1. URL検証/metadata取得→GitHub repository IDとcommit SHA。
2. repositories upsert。
3. `(repository_id, commit_sha, analyzer_version)`でinsert with `ON CONFLICT DO NOTHING`。
4. conflict時はexisting rowをSELECT（`completed`なら200 + cached=true、`queued/running`なら202）。
5. `failed`を同じキーで再試行させる場合はattempt回数/再試行時間/leaseを慎重に制御する。MVPはfailedを返してUIへエラー表示、無制限再試行はさせない。

### atomic save
解析はメモリ上で完了させてから、トランザクション内にsource_files→import_references→集計→status=completedの順で書き込む。失敗時は全体をrollback。部分結果を完成済みとして返さない。

## 5. graph取得SQLの考え方
内部エッジは次のように取得。`source_file_id=target_file_id`のself-importはグラフ表示から除外し、カウント方針をテストで固定する。

```sql
SELECT DISTINCT source_file_id, target_file_id
FROM import_references
WHERE analysis_id = $1
  AND classification = 'internal'
  AND target_file_id IS NOT NULL
  AND source_file_id <> target_file_id
ORDER BY source_file_id, target_file_id;
```

## 6. 将来拡張
- repository analysis history / compare old/new SHA
- `symbols`/`calls` table（関数・クラス単位グラフ）
- `file_embeddings`/`summaries`（AI要約）
- `analysis_events`（ジョブの詳細進捗、失敗診断）

MVPではこれらを先行実装しない。
