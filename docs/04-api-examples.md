# RepoAtlas — APIリクエスト/レスポンス例 v1.0

## 1. ヘルスチェック (`GET /healthz`)
```bash
curl -i http://localhost:8080/healthz
```
Response (`200 OK`):
```json
{"status":"ok"}
```

## 2. リポジトリ解析ジョブ登録 (`POST /api/v1/analyses`)
```bash
curl -i -X POST http://localhost:8080/api/v1/analyses \
  -H "Content-Type: application/json" \
  -d '{"repository_url":"https://github.com/pallets/click"}'
```
Response (`202 Accepted` 新規/処理中、完了済みキャッシュヒット時は `200 OK` + `"cached": true`):
```json
{
  "analysis_id": "3c7c0834-8d35-483e-8d95-9f8e7d6c5b4a",
  "status": "queued",
  "cached": false
}
```

## 3. ジョブ状態取得 (`GET /api/v1/analyses/{id}`)
```bash
curl -s http://localhost:8080/api/v1/analyses/3c7c0834-8d35-483e-8d95-9f8e7d6c5b4a | jq .
```
Response (`200 OK`):
```json
{
  "id": "3c7c0834-8d35-483e-8d95-9f8e7d6c5b4a",
  "status": "running",
  "stage": "parsing",
  "repository": {
    "owner": "pallets",
    "name": "click",
    "html_url": "https://github.com/pallets/click"
  },
  "commit_sha": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
  "error": {
    "code": null,
    "message": null
  },
  "created_at": "2026-10-30T12:00:00Z",
  "completed_at": null
}
```

## 4. 依存グラフ取得 (`GET /api/v1/analyses/{id}/graph`)
```bash
curl -s http://localhost:8080/api/v1/analyses/3c7c0834-8d35-483e-8d95-9f8e7d6c5b4a/graph | jq .
```
Response (`200 OK`, 未完了の場合は `409 ANALYSIS_NOT_READY`):
```json
{
  "analysis_id": "3c7c0834-8d35-483e-8d95-9f8e7d6c5b4a",
  "repository": {
    "owner": "owner",
    "name": "repo",
    "html_url": "https://github.com/owner/repo"
  },
  "commit_sha": "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
  "summary": {
    "files": 3,
    "internal_edges": 3,
    "external_imports": 1,
    "unresolved_imports": 0
  },
  "nodes": [
    {
      "id": "file:app/main.py",
      "path": "app/main.py",
      "functions": 1,
      "classes": 0,
      "in_degree": 0,
      "out_degree": 2,
      "is_entrypoint": true,
      "importance_score": 4,
      "github_url": "https://github.com/owner/repo/blob/a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2/app/main.py"
    }
  ],
  "edges": [
    {
      "source": "file:app/main.py",
      "target": "file:app/service.py",
      "kind": "import"
    }
  ],
  "recommended_file_ids": [
    "file:app/main.py"
  ],
  "warnings": [
    "Static import graph; dynamic imports are not included."
  ]
}
```

## 5. デモグラフ取得 (`GET /api/v1/demo/graph`)
```bash
curl -s http://localhost:8080/api/v1/demo/graph | jq .
```
Response (`200 OK`, `"demo": true` 付きで同梱サンプルデータを返却)。
