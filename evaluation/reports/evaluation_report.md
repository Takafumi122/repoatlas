# RepoAtlas Static Analysis Evaluation Report

- **Generated At**: `2026-11-05T03:07:39Z`
- **Go Runtime**: `go1.27.3` (10 CPUs)
- **Total Projects Evaluated**: `17`

## Overall Benchmark Summary

| Metric | Value |
| :--- | :--- |
| **Overall Precision** | **100.00%** |
| **Overall Recall** | **100.00%** |
| **Overall F1-Score** | **100.00%** |
| **Average Unresolved Rate** | **3.92%** |
| **Total Execution Time** | **5.09 ms** |
| **Peak Heap Allocated** | **0.41 MB** |

## Project Breakdown

| Project | Dataset | Files | Imports | Precision | Recall | F1 | Unresolved | Latency (ms) | Peak Heap (KB) |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `click` | `real_repositories` | 8 | 14 | 100.0% | 100.0% | 100.0% | 0.0% | 1.40 | 326.0 |
| `itsdangerous` | `real_repositories` | 8 | 34 | 100.0% | 100.0% | 100.0% | 0.0% | 1.69 | 421.4 |
| `requests` | `real_repositories` | 13 | 23 | 100.0% | 100.0% | 100.0% | 0.0% | 1.06 | 390.7 |
| `01_stdlib` | `synthetic` | 1 | 3 | 100.0% | 100.0% | 100.0% | 0.0% | 0.07 | 266.8 |
| `02_thirdparty` | `synthetic` | 1 | 4 | 100.0% | 100.0% | 100.0% | 0.0% | 0.07 | 267.9 |
| `03_relative_same` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.06 | 267.1 |
| `04_relative_parent` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.06 | 267.6 |
| `05_relative_deep` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.05 | 267.6 |
| `06_init_reexport` | `synthetic` | 3 | 2 | 100.0% | 100.0% | 100.0% | 0.0% | 0.07 | 271.2 |
| `07_aliased` | `synthetic` | 3 | 2 | 100.0% | 100.0% | 100.0% | 0.0% | 0.08 | 273.8 |
| `08_multi_symbol` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.08 | 272.7 |
| `09_conditional` | `synthetic` | 4 | 5 | 100.0% | 100.0% | 100.0% | 0.0% | 0.13 | 283.2 |
| `10_dynamic` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.07 | 269.8 |
| `11_star_import` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.04 | 269.0 |
| `12_shadowed` | `synthetic` | 2 | 1 | 100.0% | 100.0% | 100.0% | 0.0% | 0.04 | 268.5 |
| `13_circular` | `synthetic` | 2 | 2 | 100.0% | 100.0% | 100.0% | 0.0% | 0.04 | 270.2 |
| `14_unresolved` | `synthetic` | 3 | 3 | 100.0% | 100.0% | 100.0% | 66.7% | 0.09 | 276.7 |

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

