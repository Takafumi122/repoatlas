import React from "react";
import { RepoUrlForm } from "@/components/RepoUrlForm";

export default function HomePage() {
  return (
    <main className="home-container">
      <header className="hero-header">
        <span className="hero-badge">
          Zero-Execution Python Architecture & Dependency Explorer
        </span>
        <h1 className="hero-title">RepoAtlas</h1>
        <p className="hero-subtitle">
          初めてのPythonコードベースを、数分で俯瞰する。
          公開GitHubリポジトリのURLを入力するだけで、コードを一切実行せずにファイル間の依存グラフ・読むべき推奨順序（Guided
          Code Tour）・変更影響範囲（Impact Analysis）を可視化します。
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
          <h3>🔒 コード非実行・安全な静的解析</h3>
          <p className="muted-text">
            リポジトリ内のPythonコードやsetup.pyは一切実行しません。固定コミットSHAのZIPアーカイブをインメモリ制限下で展開し、Go
            + Tree-sitterで構文木を解析します。
          </p>
        </div>
        <div className="card feature-item">
          <h3>🧭 決定論的 Guided Code Tour</h3>
          <p className="muted-text">
            単純な被依存数順ではなく、エントリーポイントからの到達距離とモジュール役割を評価し、5〜10件の推奨閲覧順序と理由を提示します。
          </p>
        </div>
        <div className="card feature-item">
          <h3>🎯 BFS Impact Analysis & ディレクトリ集約</h3>
          <p className="muted-text">
            循環依存に対応した幅優先探索（BFS）で潜在的影響範囲を深さ別に可視化。大規模リポジトリでもArchitecture
            Viewでパッケージ構造を即座に把握できます。
          </p>
        </div>
      </section>
    </main>
  );
}
