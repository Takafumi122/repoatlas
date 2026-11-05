import React from "react";
import { RepoUrlForm } from "@/components/RepoUrlForm";

export default function HomePage() {
  return (
    <main className="home-container">
      <header className="hero-header">
        <span className="hero-badge">
          Zero-Execution Python & Go Architecture & Dependency Explorer
        </span>
        <h1 className="hero-title">RepoAtlas</h1>
        <p className="hero-subtitle">
          初めてのコードベースを、数分で俯瞰する。
          公開GitHubリポジトリ（Python & Go）のURLを入力するだけで、コードを一切実行せずに依存グラフ・読むべき推奨順序（Guided
          Code Tour）・変更影響範囲（Impact Analysis）・2コミット間のアーキテクチャ差分（Architecture Diff）を可視化します。
        </p>
      </header>

      <RepoUrlForm />

      <section className="onboarding-steps" aria-label="使い方の流れ">
        <div className="step-card">
          <span className="step-num-badge">STEP 1</span>
          <h3>GitHub URLを入力 / サンプルを選択</h3>
          <p className="muted-text">
            公開PythonリポジトリのURLを入力するか、ワンクリックのサンプル（itsdangerous /
            requests）または同梱デモを開きます。
          </p>
        </div>
        <div className="step-card">
          <span className="step-num-badge">STEP 2</span>
          <h3>Guided Code Tour で入口から理解</h3>
          <p className="muted-text">
            「Entrypoint → 主要制御フロー → 中核モデル」の推奨順に沿って Next /
            Previous で重要ファイルを順番に辿れます。
          </p>
        </div>
        <div className="step-card">
          <span className="step-num-badge">STEP 3</span>
          <h3>Impact Analysis & Architecture View</h3>
          <p className="muted-text">
            BFSによる変更影響範囲（直接・間接依存元）のハイライトや、ディレクトリ単位の集約グラフへの切り替えが可能です。
          </p>
        </div>
      </section>

      <section className="feature-grid" aria-label="主な特徴">
        <div className="card feature-item">
          <h3>🔒 コード非実行・安全な静的解析 (Python & Go)</h3>
          <p className="muted-text">
            リポジトリ内のコードは一切実行しません。PythonはTree-sitter、GoはGo標準パーサ（go/parser・go/ast）とgo.mod解析を活用し、Zero-Executionで高速に依存関係を抽出します。
          </p>
        </div>
        <div className="card feature-item">
          <h3>🧭 決定論的 Guided Code Tour</h3>
          <p className="muted-text">
            エントリーポイント（main.py / package main）からの到達距離とモジュール役割を評価し、5〜10件の推奨閲覧順序と理由を決定論的に提示します。
          </p>
        </div>
        <div className="card feature-item">
          <h3>⚡ Architecture Diff & 循環依存（SCC）検知</h3>
          <p className="muted-text">
            BASEとHEADの2コミット間で依存グラフの変化を比較。ノードやエッジの追加・削除・変更に加え、Tarjan SCCアルゴリズムにより循環依存の発生・解消を即座に特定します。
          </p>
        </div>
      </section>
    </main>
  );
}
