# RepoAtlas Railway デプロイ手順書（Phase 4）

本書は、RepoAtlas（Monorepo構成）を [Railway](https://railway.com/) 上に4サービス構成（**PostgreSQL / API / Worker / Frontend**）として安全にデプロイ・運用するための手順書です。

---

## 全体アーキテクチャ（Railway 4サービス構成）

```mermaid
flowchart LR
    Browser["User Browser"] -->|"HTTPS (Public Domain)"| Web["1. Frontend Service<br/>(Next.js :$PORT)"]
    Web -->|"/api/v1/* Rewrite Proxy<br/>(Private Network)"| API["2. API Service<br/>(Go /app/api :$PORT)"]
    API -->|"DATABASE_PRIVATE_URL<br/>(Private Network)"| PG[("4. PostgreSQL 16<br/>(Private Only)")]
    Worker["3. Worker Service<br/>(Go /app/worker)"] -->|"DATABASE_PRIVATE_URL<br/>(Private Network)"| PG
    API -->|"HTTPS + GITHUB_TOKEN"| GH["GitHub REST API"]
    Worker -->|"HTTPS + GITHUB_TOKEN"| GH
```

- **パブリック公開するのは `Frontend` サービスのみ**（推奨構成）です。
- ブラウザからの `/api/v1/*` リクエストは Next.js の `rewrites()` によって同一オリジンで受け付け、Railway 内部プライベートネットワーク（`http://api.railway.internal:8080`）経由で `API` サービスへ転送されます。
- これにより、ブラウザが `.railway.internal` へ直接アクセスして名前解決エラーになる問題や、CORS プリフライトのオーバーヘッド、`API` や `PostgreSQL` の不要なパブリック露出を防ぎます。

---

## 1. Railway プロジェクト作成

1. [Railway Dashboard](https://railway.com/dashboard) にログインします。
2. 右上の **New Project** をクリックします。
3. **Empty Project** を選択して新規プロジェクト（例: `repoatlas-prod`）を作成します。
4. プロジェクト設定（**Settings > Environments**）で環境名が `production` であることを確認します（Railway は自動で `RAILWAY_ENVIRONMENT_NAME=production` を注入します）。

---

## 2. GitHub リポジトリ接続

1. Railway Account Settings > **Source Control** から GitHub アカウントを接続し、対象の `repoatlas` リポジトリへのアクセス権限を付与します。
2. `main` ブランチへの Push で自動デプロイ（Auto Deploy）される構成にします。

---

## 3. 4サービスの追加

プロジェクトキャンバス上で **+ New** ボタンから以下の順で4つのサービスを追加します。

| 順序 | サービス名（推奨） | 種別 | Railway Root Directory | 設定ファイルパス（Config as Code） |
| :--- | :--- | :--- | :--- | :--- |
| **1** | `postgres` | Database > **Add PostgreSQL** | —（マネージドDB） | — |
| **2** | `api` | **GitHub Repo** (`repoatlas`) | `/backend` | `/backend/railway.api.toml` |
| **3** | `worker` | **GitHub Repo** (`repoatlas`) | `/backend` | `/backend/railway.worker.toml` |
| **4** | `web` | **GitHub Repo** (`repoatlas`) | `/frontend` | `/frontend/railway.toml` |

> **重要（Monorepo の Build Context）**
> `api` と `worker` は同じ `/backend` ディレクトリおよび `backend/Dockerfile` を共有し、起動コマンド（`/app/api` と `/app/worker`）のみを分けて独立起動します。必ず各サービスの **Settings > Source > Root Directory** に `/backend` または `/frontend` を設定してください。

---

## 4. Dockerfile と Start Command 設定

各サービスの設定ファイル（`railway.*.toml`）または Railway GUI の **Settings > Build / Deploy** に以下を設定します。

### 4.1 `api` サービス
- **Root Directory**: `/backend`
- **Config File Path**: `/backend/railway.api.toml`
- **Dockerfile Path**: `Dockerfile`
- **Start Command**: `/app/api`
- **Healthcheck Path**: `/healthz`（Timeout: `30s`）
- **Replicas**: `1`（インメモリIPレート制限の一貫性を保つため単一レプリカ固定）

### 4.2 `worker` サービス
- **Root Directory**: `/backend`
- **Config File Path**: `/backend/railway.worker.toml`
- **Dockerfile Path**: `Dockerfile`
- **Start Command**: `/app/worker`
- **Healthcheck Path**: なし（バックグラウンドワーカーのため HTTP ポートはリッスンしません）
- **Replicas**: `1`（必要に応じて水平スケール可能ですが、初期構成は `1` を推奨）

### 4.3 `web`（Frontend）サービス
- **Root Directory**: `/frontend`
- **Config File Path**: `/frontend/railway.toml`
- **Dockerfile Path**: `Dockerfile`
- **Start Command**: `sh -c './node_modules/.bin/next start -H 0.0.0.0 -p ${PORT:-3000}'`
- **Healthcheck Path**: `/`（Timeout: `30s`）
- **Replicas**: `1`

### 4.4 Graceful Shutdown の動作
- Railway はデプロイ更新や停止時にコンテナへ `SIGTERM` を送信します。
- `api` サービスは `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)` でシグナルを捕捉し、新規リクエスト受付を停止した上で最大10秒間の Graceful Shutdown を行います。
- `worker` サービスも `SIGTERM` 受信時に新規ジョブのポーリングを停止し、実行中ジョブのコンテキストおよびハートビートを安全に終了します（万が一中断されたジョブも `lease_expires_at` 経過後に新 Worker が自動回復します）。

---

## 5. 環境変数一覧

Railway の Reference Variables（`${{...}}`）機能を利用して、ハードコードを避けて設定します。

### 5.1 `postgres` サービス
- デフォルト作成時の変数（`POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `DATABASE_PRIVATE_URL`, `DATABASE_URL`）をそのまま利用します。
- **Networking > Public Networking（TCP Proxy）は有効化しないでください**（内部通信のみに限定）。

### 5.2 `api` サービス

| 変数名 | 設定値（Railway推奨値） | 説明 |
| :--- | :--- | :--- |
| `APP_ENV` | `production` | 本番モードを有効化（`fixture/*` フォールバック等を完全無効化） |
| `DATABASE_PRIVATE_URL` | `${{postgres.DATABASE_PRIVATE_URL}}` | PostgreSQL プライベート接続URL（`DATABASE_URL` より優先） |
| `PORT` | `8080` | Railway が注入するポート（`API_ADDR` 未指定時に自動で `:8080` で待受） |
| `AUTO_MIGRATE` | `true` | **API起動時にのみ** 冪等なSQLマイグレーションを実行 |
| `GITHUB_TOKEN` | `<GitHub Fine-grained PAT>` | GitHub API レート制限を 60回/時 → 5,000回/時 へ引き上げ（Secret指定） |
| `ALLOWED_ORIGINS` | `https://${{web.RAILWAY_PUBLIC_DOMAIN}}` | CORS 許可オリジン（カンマ区切り可） |
| `ANALYZER_VERSION` | `v1.0.0` | 解析エンジンバージョン（キャッシュキーに使用） |
| `POST_ANALYSES_RATE_LIMIT_PER_MIN` | `10` | IPあたり1分間の `POST /api/v1/analyses` 上限回数 |
| `MAX_QUEUED_JOBS` | `25` | クラスタ全体の同時 `queued` + `running` ジョブ上限 |
| `TRUSTED_PROXIES` | `100.64.0.0/10,10.0.0.0/8,172.16.0.0/12` | Railway エッジ/内部プロキシからの `X-Forwarded-For` を信頼 |

### 5.3 `worker` サービス

| 変数名 | 設定値（Railway推奨値） | 説明 |
| :--- | :--- | :--- |
| `APP_ENV` | `production` | 本番モードを有効化（`fixture/*` フォールバック等を完全無効化） |
| `DATABASE_PRIVATE_URL` | `${{postgres.DATABASE_PRIVATE_URL}}` | PostgreSQL プライベート接続URL |
| `AUTO_MIGRATE` | `false` | **単一実行方針**: マイグレーション競合防止のため Worker 側は `false` |
| `GITHUB_TOKEN` | `<GitHub Fine-grained PAT>` | GitHub ZIP アーカイブ取得用トークン（Secret指定） |
| `ANALYZER_VERSION` | `v1.0.0` | `api` と同一バージョンを指定 |
| `WORKER_CONCURRENCY` | `1` | 1コンテナあたりの同時解析ワーカー数（メモリ保護のため `1` 推奨） |
| `WORKER_POLL_INTERVAL` | `2s` | DB キューのポーリング間隔 |
| `ANALYSIS_TIMEOUT` | `90s` | 1ジョブあたりのハードタイムアウト |

### 5.4 `web`（Frontend）サービス

| 変数名 | 設定値（Railway推奨値） | 説明 |
| :--- | :--- | :--- |
| `NEXT_PUBLIC_API_BASE_URL` | `same-origin` | ブラウザから同一オリジン `/api/v1/*` へ通信させる設定（ビルド時＆実行時） |
| `INTERNAL_API_BASE_URL` | `http://${{api.RAILWAY_PRIVATE_DOMAIN}}:${{api.PORT}}` | Next.js サーバーが Railway 内部通信で `api` へプロキシする宛先 |
| `PORT` | `3000` | Next.js サーバーの待受ポート |

> **補足（別オリジンで API を直接公開したい場合）**
> もし `api` サービスにも Public Domain を付与してブラウザから直接呼び出す場合は、`web` の `NEXT_PUBLIC_API_BASE_URL=https://${{api.RAILWAY_PUBLIC_DOMAIN}}` を設定して `web` を再ビルドしてください。ただし、セキュリティと通信効率の観点からは上記の `same-origin` + `INTERNAL_API_BASE_URL` 方式を強く推奨します。

---

## 6. パブリックドメイン設定

1. `web` サービスの **Settings > Networking > Public Networking** を開きます。
2. **Generate Domain** をクリックして `https://<project-name>.up.railway.app` ドメインを発行します（独自ドメインを使う場合は **Custom Domain** を追加し CNAME レコードを設定します）。
3. `postgres` と `worker` の Public Networking は **無効（Disabled）** のままにします。
4. `api` の Public Networking も、`same-origin` プロキシ方式では **無効（Disabled）** のままで動作します（外部から直接 `curl` で API を叩きたい場合のみ有効化してください）。

---

## 7. マイグレーション実行手順（単一実行方針）

RepoAtlas は複数コンテナからのマイグレーション競合を防ぐため、以下の単一実行方針を採用しています。

1. **本番環境での標準方針**:
   - `api` サービスのみ `AUTO_MIGRATE=true`（単一レプリカ）とし、起動時に `backend/migrations/*.up.sql`（`000001_init.up.sql`, `000002_lease_owner.up.sql`）をトランザクション内で順次適用します。
   - `worker` サービスは `AUTO_MIGRATE=false` とし、スキーマ変更を実行しません。
   - すべてのマイグレーションSQLは `CREATE TABLE IF NOT EXISTS` / `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` で冪等に記述されています。
2. **手動でマイグレーションを確認したい場合**:
   - Railway CLI（`railway connect postgres`）で接続し、`\dt` で `repositories`, `analysis_runs`, `files`, `symbols`, `dependencies` テーブルが存在することを確認します。

---

## 8. 初回デプロイ確認手順

デプロイ完了後（すべてのサービスが `Active` / Green になった後）、手元のターミナルから付属のスモークテストスクリプトを実行します。

```bash
# パブリック公開された Frontend URL に対してスモークテストを実行
./scripts/smoke-test.sh https://<your-web-domain>.up.railway.app
```

またはブラウザで `https://<your-web-domain>.up.railway.app` を開き、以下を確認します。
1. トップページが表示され、**デモを見る（0秒で体験）** ボタンから `/demo` が即座に描画されること。
2. サンプルリポジトリ `https://github.com/pallets/itsdangerous` をクリックして解析を開始し、数秒で依存グラフ・Guided Code Tour・Impact Analysis・Architecture View が動作すること。
3. もう一度同じリポジトリを送信した際に、キャッシュヒット（`200 OK` / 即時完了）すること。

---

## 9. ログ確認方法

1. Railway Dashboard で各サービス（`api`, `worker`, `web`, `postgres`）をクリックし、**Deployments > View Logs** タブを開きます。
2. `api` と `worker` は Go の `log/slog` による構造化ログを出力します。
   - `starting RepoAtlas API server` / `starting RepoAtlas worker` に `env=production` が出力されていることを確認します。
   - `backend/internal/observability/logger.go` の `RedactingHandler` により、`token`, `authorization`, `password`, `secret`, `dsn`, `database_url` を含むキーや `ghp_*` / `github_pat_*` / `postgres://user:pass@...` 形式の文字列は自動的に `[REDACTED]` にマスクされ、ログに秘密情報が一切出力されない設計になっています。

---

## 10. ロールバック手順

デプロイ後に問題が発生した場合は、以下の手順で即座にロールバックできます。

1. **アプリケーション（API / Worker / Frontend）のロールバック**:
   - Railway Dashboard で対象サービスを選択 > **Deployments** タブを開きます。
   - 正常動作していた直前の Deployment の右メニュー（`⋮`）をクリックし、**Rollback**（または **Redeploy**）を選択します（数十秒で以前のコンテナイメージに切り替わります）。
2. **データベーススキーマのロールバック（必要な場合のみ）**:
   - `backend/migrations/000002_lease_owner.down.sql` および `000001_init.down.sql` にロールバック用SQLが用意されています。

---

## 11. 月額上限・使用量確認方法と想定運用コスト

### 11.1 使用量アラートとハードリミットの設定手順
予期せぬ課金を防ぐため、初回デプロイ時に必ず Railway の Usage Limit を設定してください。

1. Railway Dashboard 左下のアカウントメニュー > **Account Settings**（または Workspace Settings）> **Usage** を開きます。
2. **Usage Limits** セクションで **Configure Usage Limit** をクリックします。
3. 以下を設定して保存します：
   - **Email Alert Threshold（通知閾値）**: `$5.00`
   - **Hard Limit（強制停止上限）**: `$10.00`（上限到達時に全サービスを自動停止し、超過課金を確実に防止）
4. さらに各サービス（`api`, `worker`, `web`）の **Settings > Deploy > Resources** でメモリ上限を `512 MB`〜`1 GB`、vCPU上限を `1 vCPU` に制限しておくことで、万が一の暴走時にもリソース消費を抑えられます。

### 11.2 想定運用コスト試算（ポートフォリオ公開・小規模利用時）

Railway Hobby Plan（基本料金 `$5.00 / 月`、`$5.00` 分のリソース使用枠込み）での試算：

| サービス | 常時メモリ使用量目安 | アイドル〜軽負荷時 CPU | 推定月額リソース消費 |
| :--- | :--- | :--- | :--- |
| **PostgreSQL 16** | 約 100〜180 MB | 0.01〜0.03 vCPU | 約 `$1.20`〜`$2.00` / 月 |
| **API (Go)** | 約 20〜40 MB | 0.01 vCPU 未満 | 約 `$0.20`〜`$0.50` / 月 |
| **Worker (Go)** | 約 25〜80 MB（解析時最大 ~200 MB） | 0.01〜0.05 vCPU | 約 `$0.30`〜`$0.80` / 月 |
| **Frontend (Next.js)** | 約 90〜160 MB | 0.01〜0.03 vCPU | 約 `$0.80`〜`$1.50` / 月 |
| **合計（4サービス）** | **約 235〜460 MB** | **軽負荷** | **約 `$2.50`〜`$4.80` / 月** |

- **結論**: ポートフォリオ公開用途（1日数件〜数十件のリポジトリ解析＋デモ閲覧）であれば、4サービス合計のリソース消費は `$5.00` 未満に収まり、**Railway Hobby Plan の基本料金 `$5.00 / 月` の範囲内（追加従量課金ほぼ `$0`）** で安定運用可能です。
