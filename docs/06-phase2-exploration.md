# 06. Phase 2 — Codebase Exploration 設計・ルール・限界

本書は RepoAtlas Phase 2 で追加された **Guided Code Tour**、**Impact Analysis**、および **Architecture View** の決定論的アルゴリズム仕様と既知の制約（静的解析の限界）を定義する。

---

## 1. Guided Code Tour（推奨閲覧順序生成）

### 1.1 設計目的
単純な被依存数（`in_degree`）順のランキングでは、ユーティリティや設定・例外定義などの末端モジュールばかりが上位に並び、初見の開発者が「どこからプログラムが始まり、どのように主要処理が流れるか」を把握しにくい。

Guided Code Tour（[`BuildGuidedTour`](../backend/internal/analyzer/graph/tour.go)）では、**「起点（Entrypoint / Public API） → 主要制御フロー（Orchestrator） → 中核モデル・共有基盤（Core / Foundation）」** の 3 段階フェーズで 5〜10 件のファイルを選択し、決定論的に閲覧順序を生成する。

### 1.2 決定論的アルゴリズムとスコアリング規則
1. **対象ステップ数の決定（`DetermineTargetTourSize`）**
   - リポジトリの総ファイル数が 5 件以下の場合は全件を選択する。
   - 6 件以上の場合は非テストファイル数に応じて `clamp(5 + (nonTestFiles - 5) / 4, 5, 10)` 件を選択する。
   - テスト・補助ファイル（`tests/`, `test_*.py`, `*_test.py`, `conftest.py`, `setup.py`, `docs/`, `examples/`）は、本番コードが十分に存在する限り候補から除外する。
2. **Phase 1：Entrypoint / Public Facade の選出（最大 1〜2 件）**
   - 候補条件：
     - 明示的エントリーポイント名（`main.py`, `app.py`, `__main__.py`, `cli.py`, `server.py`, `run.py`, `wsgi.py`, `asgi.py`, `manage.py`）：`+6.0`
     - パッケージ公開境界となる浅い階層の `__init__.py`（`depth <= 2` かつ `in_degree > 0 || out_degree > 0`）：`+4.2`
     - 被依存 0 で内部モジュールを呼び出す起点モジュール（`in_degree == 0 && out_degree > 0`）：`+3.2`
   - 加算・減点：`+0.9 * out_degree + 0.4 * in_degree + min(2.0, 0.15 * (functions + classes)) - 0.35 * directory_depth`
   - 同点時は `path ASC` で決定論的にタイブレークする。
3. **Phase 2：Orchestrator / 主要制御フローの選出**
   - Phase 1 で選ばれた起点ファイル群から順方向（import 方向 `source -> target`）に **幅優先探索（BFS）** を行い、各ファイルへの最短到達ホップ数 `entryDist` を算出する。
   - `out_degree >= 1` の非テストファイルを対象に以下で評価する：
     - `entryDist == 1`（起点から直接 import）：`+3.5`
     - `entryDist == 2`（起点から 2 ホップ）：`+1.8`
     - ハブボーナス（`in_degree >= 1 && out_degree >= 2`）：`+2.0`
     - `+1.4 * out_degree + 1.1 * in_degree + min(2.5, 0.2 * (functions + classes)) - 0.25 * directory_depth`
   - 同点時は `entryDist ASC`、次に `path ASC` で決定論的にソートする。
4. **Phase 3：Core Domain / Shared Foundation の選出**
   - 前半ステップ（Phase 1・Phase 2）から直接 import されている回数 `tourRefCount` と全体の `in_degree` を重視して残りの枠を埋める：
     - `+2.2 * in_degree + 0.6 * out_degree + 1.2 * tourRefCount + min(3.0, 0.25 * (functions + classes)) - 0.20 * directory_depth`
     - `in_degree >= 2 && out_degree == 0` のファイルは `foundation`（共有基盤）、それ以外は `core_module`（中核モデル）と分類する。
   - 同点時は `importance_score DESC`、次に `path ASC` で決定論的にソートする。

### 1.3 Guided Code Tour の限界
- Tree-sitter による静的 `import` / `from ... import` 構文のみを入力とするため、フレームワークの DI（依存性注入）、文字列指定のルーティング、動的 import（`importlib`）、デコレータによる暗黙登録はグラフエッジに反映されない。
- ファイル名・ディレクトリ階層・次数・関数/クラス定義数に基づくヒューリスティックであり、ドメイン固有のビジネス重要度を保証するものではない。

---

## 2. Impact Analysis（BFS による影響範囲解析）

### 2.1 アルゴリズム仕様（[`AnalyzeImpact`](../backend/internal/analyzer/graph/impact.go)）
- 対象ファイル `T` が変更された際に影響を受ける可能性のあるファイル群を特定するため、内部 import エッジ `S -> T`（`S` が `T` を import している）の**逆方向エッジ（`T -> S`）**を辿る **幅優先探索（BFS）** を実行する。
- **直接依存元（Direct Dependents）**：`depth == 1`（`T` を直接 import しているファイル）
- **間接依存元（Indirect Dependents）**：`2 <= depth <= maxDepth`（直接依存元をさらに import している上位ファイル）
- **循環依存への安全性**：`visitedDepth` マップで各ファイルの初回発見時の最短距離を記録し、既に訪問済みのノードは再キューイングしない。これにより 2 ノード相互依存（`A <-> B`）や多段サイクル（`A -> B -> C -> A`）が存在しても無限ループに陥らず `O(V + E)` 時間で終了する。
- **決定論的順序**：各頂点の隣接リストを事前に `path ASC` でソートしてから BFS を行い、結果一覧は `Depth ASC -> ImportanceScore DESC -> Path ASC` で安定ソートする。

### 2.2 Impact Analysis の限界
- 静的 import グラフ上の到達可能性に基づく**潜在的影響範囲**であり、シンボル単位の使用有無や実行時のコールグラフ、リフレクション・動的ロードによる影響は保証しない。

---

## 3. Architecture View（ディレクトリ単位の集約グラフ）

### 3.1 アルゴリズム仕様（[`BuildArchitectureGraph`](../backend/internal/analyzer/graph/architecture.go)）
- 各ファイルの所属ディレクトリ `path.Dir(file.Path)`（ルート直下は `"(root)"`）をキーとしてファイルをグループ化する。
- 同一ディレクトリ内のファイル間 import エッジは `internal_edge_count`（内部凝集エッジ数）としてディレクトリノードに集計し、異なるディレクトリ間の import エッジは `ArchitectureEdge` の `weight`（依存件数）および代表ペア `sample_pairs`（最大 5 件）として集約する。
- 大規模リポジトリ（数百ファイル規模）でもディレクトリ単位に圧縮することで、パッケージ間の結合度と依存の向きを高速に俯瞰できる。
