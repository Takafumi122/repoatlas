# RepoAtlas Static Analysis Evaluation Report

- **Generated At**: `2026-11-04T13:52:04Z`
- **Go Runtime**: `go1.27.3` (10 CPUs)
- **Total Projects Evaluated**: `17`

## Overall Benchmark Summary

| Metric | Value |
| :--- | :--- |
| **Overall Precision** | **100.00%** |
| **Overall Recall** | **100.00%** |
| **Overall F1-Score** | **100.00%** |
| **Average Unresolved Rate** | **3.92%** |
| **Total Execution Time** | **4.64 ms** |
| **Peak Heap Allocated** | **0.37 MB** |

## Project Breakdown

| Project | Dataset | Files | Imports | Precision | Recall | F1 | Unresolved | Latency (ms) | Peak Heap (KB) |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `click` | `real_repositories` | 8 | 14 | 100.0% | 100.0% | 100.0% | 0.0% | 1.37 | 308.7 |
| `itsdangerous` | `real_repositories` | 8 | 34 | 100.0% | 100.0% | 100.0% | 0.0% | 1.16 | 383.4 |
| `requests` | `real_repositories` | 13 | 23 | 100.0% | 100.0% | 100.0% | 0.0% | 1.17 | 372.7 |
| `01_stdlib` | `synthetic` | 1 | 3 | 100.0% | 100.0% | 100.0% | 0.0% | 0.06 | 257.3 |
| `02_thirdparty` | `synthetic` | 1 | 4 | 100.0% | 100.0% | 100.0% | 0.0% | 0.08 | 257.6 |
| `03_relative_same` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.05 | 256.6 |
| `04_relative_parent` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.06 | 256.8 |
| `05_relative_deep` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.06 | 256.9 |
| `06_init_reexport` | `synthetic` | 3 | 2 | 100.0% | 100.0% | 100.0% | 0.0% | 0.07 | 260.5 |
| `07_aliased` | `synthetic` | 3 | 2 | 100.0% | 100.0% | 100.0% | 0.0% | 0.08 | 262.1 |
| `08_multi_symbol` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.08 | 261.5 |
| `09_conditional` | `synthetic` | 4 | 5 | 100.0% | 100.0% | 100.0% | 0.0% | 0.13 | 269.9 |
| `10_dynamic` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.07 | 258.7 |
| `11_star_import` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.04 | 263.9 |
| `12_shadowed` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.04 | 263.8 |
| `13_circular` | `synthetic` | 2 | 2 | 100.0% | 100.0% | 100.0% | 0.0% | 0.04 | 264.8 |
| `14_unresolved` | `synthetic` | 3 | 3 | 100.0% | 100.0% | 100.0% | 66.7% | 0.10 | 271.7 |

## Dataset Scope & Limitations: Full vs Partial Repository Distinction

- **Synthetic Datasets (`01_stdlib` ~ `14_unresolved`)**: 完全に制御された構文網羅テストケース群。構文木抽出、相対import境界、エイリアス、循環依存、未解決エラーの検知精度を独立検証。
- **Real OSS Repositories (`click`, `itsdangerous`, `requests`)**: 本データセットは実リポジトリの全ファイル（テストスイート・ドキュメント・設定等を含む全体）ではなく、**コアパッケージディレクトリ（`src/` 配下）のモジュール群を対象とした部分評価（Partial Core Package Evaluation）** です。
  - `pallets/itsdangerous`: 全15ファイル中、コアパッケージ `src/itsdangerous/` の8ファイル（22内部エッジ）を評価対象として抽出（テスト群7ファイルを除外）。
  - `psf/requests`: 全37ファイル中、コアパッケージ `src/requests/` の主要13ファイル（20内部エッジ）を評価対象として抽出。
  - `pallets/click`: 全22ファイル中、中核モジュール `src/click/` の8ファイル（13内部エッジ）を評価対象として抽出。

## Error Pattern Categorization

### `14_unresolved` (`synthetic`)

**Unresolved Imports:**
- `mypkg/broken.py:2 (..outside.non_existent) - 相対モジュールが見つかりません: outside`
- `mypkg/broken.py:5 (mypkg.ghost_module.missing_symbol) - 内部トップレベルパッケージ配下ですが該当モジュールが見つかりません: mypkg.ghost_module`

