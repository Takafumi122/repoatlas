# RepoAtlas — Go Static Analysis Specification & Architecture

## 1. 概要 (Overview)

RepoAtlas Phase 6 では、従来のPython構文解析（Tree-sitter）に加え、**Go言語コードベースの完全静的解析（Zero-Execution Static Analysis）**をネイティブ実装しました。
GitHubから取得した公開リポジトリのソースコードに対して、一切のコンパイルやコマンド実行を行わず、Go標準ライブラリ（`go/parser`, `go/ast`, `go/token`）および公式モジュールパーサ（`golang.org/x/mod/modfile`）のみを用いて、Package単位の依存関係グラフ、関数・メソッド・型定義の集計、モジュール境界、および依存の根拠証跡（Explainable Evidence）を決定論的（Deterministic）に抽出します。

---

## 2. Zero-Execution（コード非実行）原則の徹底

RepoAtlasはWebサービスとして任意のユーザー入力リポジトリを処理するため、セキュリティおよびサンドボックス保護が最優先されます。
対象リポジトリに対して以下のようなコマンドは**一切実行しません**。

- ❌ `go run` / `go build` / `go test`
- ❌ `go generate`
- ❌ `go list` / `go list -m all`
- ❌ `go mod download` / `go mod tidy`
- ❌ その他、対象コードを実行・コンパイル・依存ダウンロードするあらゆる外部プロセス

GitHub REST APIからストリーミング取得したZIPアーカイブをメモリ制限（Max 250MB, 最大5,000ファイル）のもとでインメモリ展開し、ASTパーサに直接渡すことで、悪意のある`init()`関数やCGOコード、ビルドスクリプトによる任意コード実行リスクを根本から排除しています。

---

## 3. なぜTree-sitterではなくGo標準パーサを採用したのか？

| 比較項目 | Go標準パーサ (`go/parser`, `go/ast`) | Tree-sitter (`tree-sitter-go`) |
| :--- | :--- | :--- |
| **言語文法追従性** | Go 1.18〜1.24の最新構文（Generics, Type Alias, Range-over-func等）に標準で完全追従 | バージョンごとの文法ファイル同期や更新ラグが発生 |
| **実行環境・CGO依存** | **CGO完全不要（Pure Go）**。クロスコンパイルやAlpine Linuxコンテナデプロイが極めて容易 | CGO必須。Cコンパイラ・libtree-sitterの動的リンクが必要 |
| **解析速度・メモリ** | AST生成がGoランタイム内でゼロコピーに近く極めて高速（< 1ms/ファイル） | Cgoバウンダリ越えのオーバーヘッドとCヒープメモリ確保が発生 |
| **コメント・ディレクティブ抽出** | `//go:build`, `// +build` などのBuild TagsやASTノードの位置（Line/Col）を正確に保持 | コメントノードの個別探索と正規表現抽出が必要 |

Go言語バックエンド自身がGoコードをパースする場合、Go標準ライブラリのパーサを利用することが最も安全かつ保守性が高く、完全な文法互換性と高スループットを提供できます。

---

## 4. Go Module & Workspace 解析 (`go.mod`, `go.work`)

### 4.1 Module Path の識別
リポジトリ内の各ディレクトリを探索し、`go.mod` をパースして `module <module-path>` を抽出します。
`golang.org/x/mod/modfile.Parse` を使用し、安全に構文木を取得します。

### 4.2 サブモジュールと複数モジュール
リポジトリ直下の `go.mod` だけでなく、モノレポ構成やサブディレクトリに存在する独立した `go.mod` も自動探索・索引化します。各モジュール配下のディレクトリには、対応するモジュールパスをプレフィックスとした完全修飾Import Pathを割り当てます。

### 4.3 `replace` ディレクティブと `go.work`
ローカル参照（例: `replace github.com/example/lib => ./lib`）が存在する場合：
1. リポジトリ内での相対パスを正規化（パストラバーサル `../` ガードを実施）
2. 参照先ディレクトリが取得済みリポジトリ内に実在する場合のみ内部モジュール・パッケージとして解決
3. リポジトリ外への参照やネットワークアクセスが必要なものは外部依存（external）として分類

### 4.4 モジュール競合時の安全性
同一のModule Pathが異なるディレクトリで重複定義されている等の曖昧な構成が検出された場合、誤った内部結合を避けるため曖昧（Ambiguous）として記録し、確証のある依存のみをグラフ化します。

---

## 5. Package単位の依存グラフ設計 (Granularity)

### 5.1 PythonとGoの差異

```text
[Python (RepoAtlas Phase 1〜5)]
  Node = ファイル単位 (file:app/service.py)
  Edge = ファイル間 import

[Go (RepoAtlas Phase 6)]
  Node = Package単位 (pkg:github.com/example/myapp/internal/service)
  Edge = Package間 import (A imports B)
```

Go言語のimport仕様はファイルではなくPackage Import Pathを参照します。1つのPackageは同一ディレクトリ内の複数のGoファイル（例: `handler.go`, `routes.go`, `middleware.go`）から構成されます。
RepoAtlasでは、同一Packageに属する全Goファイルの定義（関数・メソッド・型）をPackageノードに集約し、グラフ上はPackage間の1本のエッジに統合します。
同時に、クリック時に各ファイル名や各import行の証跡を確認できる階層構造を保持しています。

### 5.2 Go Package ノードのスキーマ

```json
{
  "id": "pkg:github.com/example/myapp/internal/service",
  "node_kind": "package",
  "language": "go",
  "path": "internal/service",
  "module_path": "github.com/example/myapp",
  "package_import_path": "github.com/example/myapp/internal/service",
  "directory_path": "internal/service",
  "package_name": "service",
  "files_count": 3,
  "functions": 8,
  "methods_count": 14,
  "types_count": 4,
  "classes": 4,
  "in_degree": 3,
  "out_degree": 1,
  "is_entrypoint": false,
  "importance_score": 6.5,
  "github_url": "https://github.com/example/myapp/tree/<sha>/internal/service",
  "files": [
    { "path": "service.go", "functions": 4, "methods": 8, "types": 2 },
    { "path": "helper.go", "functions": 4, "methods": 6, "types": 2 }
  ]
}
```

---

## 6. Build Constraints（ビルド条件）の取り扱い

Goコードでは `//go:build`、`// +build`、ファイル名規約（`*_linux.go`, `*_windows.go`, `*_test.go`）により、特定のOSやアーキテクチャでのみ有効となるコードが存在します。

RepoAtlasでは**静的に確認できる全依存を収集**しますが、以下の安全策を講じています：
1. 各ファイルおよびimport証跡に `build_constraints`（例: `windows`, `!linux`, `test`）をメタデータとして保持
2. UI上のPackage詳細パネルおよびエッジ情報に「Build tags」を表示し、条件付き依存であることを明示
3. 排他的なビルド条件（例: `windows` と `linux`）で成立する構成を、単一の確定した実行構成と誤認させない説明注記を付与

---

## 7. Guided Code Tour & Package Impact Analysis

### 7.1 Go Guided Code Tour
Goリポジトリの推奨閲覧順序は、以下の決定論的ヒューリスティックに基づいてスコアリングされます：
1. **Entrypoint Package**: `package main` または `cmd/` 配下のパッケージを最優先（Bonus +3.0）
2. **Orchestrator**: In-degreeとOut-degreeのバランスが高く、内部Packageを統制するレイヤー（`internal/api`, `internal/server` など）
3. **Core Domain / Service**: ビジネスロジックを担うPackage（In-degree高）
4. **Foundation / Store / Util**: 外部DBや共通ユーティリティ（Out-degree低、基盤）

### 7.2 Package Impact Analysis (BFS)
変更対象のGo Packageを指定すると、そのPackageをimportしている他のPackageを逆方向グラフ探索（Reverse BFS）によって最大指定深度まで追跡します。
PR Impact Analysisでは、変更された `.go` ファイルから所属Packageを逆引きし、Multi-source BFSを実行します。UI上には「ファイル単位の変更からPackage単位の潜在的影響を推定している」旨の注記を明示しています。

---

## 8. Explainable Dependencies（根拠証跡）

APIエンドポイント `GET /api/v1/analyses/{id}/evidence` により、グラフ上のすべての依存関係について以下の詳細証跡を提供します：

```json
{
  "source_file": "internal/api/handler.go",
  "source_package": "github.com/example/myapp/internal/api",
  "target_package": "github.com/example/myapp/internal/service",
  "import_text": "import svc \"github.com/example/myapp/internal/service\"",
  "alias": "svc",
  "line_number": 14,
  "column_number": 2,
  "classification": "internal",
  "build_constraints": "none"
}
```

UI上でノードまたはエッジを選択すると、GitHub上の該当ファイル・行番号へのダイレクトリンクとともに、実際のimport文がコードハイライト表示されます。
