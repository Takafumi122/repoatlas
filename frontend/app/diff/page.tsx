"use client";

import React, { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { APIClientError, createArchitectureDiff } from "@/lib/api";

const SAMPLE_DIFFS = [
  {
    title: "spf13/cobra (Go)",
    desc: "v1.8.0 → v1.8.1 (Go Package Architecture Diff)",
    url: "https://github.com/spf13/cobra",
    baseSha: "2077efab5d203e8ff60293674fe8c6ef1fcad243",
    headSha: "cd378035b3769971bc18bfb672778c187be06ad9",
  },
  {
    title: "pallets/itsdangerous (Python)",
    desc: "2.1.2 → 2.2.0 (Python Module Architecture Diff)",
    url: "https://github.com/pallets/itsdangerous",
    baseSha: "38198f126dae7403333333333333333333333333",
    headSha: "4444444444444444444444444444444444444444",
  },
];

export default function DiffPage() {
  const router = useRouter();
  const [repoUrl, setRepoUrl] = useState("");
  const [baseSha, setBaseSha] = useState("");
  const [headSha, setHeadSha] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmedUrl = repoUrl.trim();
    const trimmedBase = baseSha.trim();
    const trimmedHead = headSha.trim();

    if (!trimmedUrl) {
      setErrorMessage("リポジトリURLを入力してください");
      return;
    }
    if (!trimmedBase || trimmedBase.length < 7) {
      setErrorMessage("有効なBASEコミットSHAを入力してください (40桁 16進数推奨)");
      return;
    }
    if (!trimmedHead || trimmedHead.length < 7) {
      setErrorMessage("有効なHEADコミットSHAを入力してください (40桁 16進数推奨)");
      return;
    }

    setSubmitting(true);
    setErrorMessage(null);

    try {
      const res = await createArchitectureDiff(trimmedUrl, trimmedBase, trimmedHead);
      router.push(`/diff/${encodeURIComponent(res.diff_id)}`);
    } catch (err) {
      if (err instanceof APIClientError) {
        setErrorMessage(err.message);
      } else {
        setErrorMessage("Architecture Diffの登録に失敗しました。サーバー通信を確認してください。");
      }
      setSubmitting(false);
    }
  }

  function applySample(sample: (typeof SAMPLE_DIFFS)[0]) {
    setRepoUrl(sample.url);
    setBaseSha(sample.baseSha);
    setHeadSha(sample.headSha);
    setErrorMessage(null);
  }

  return (
    <main className="home-container">
      <header className="hero-header">
        <span className="hero-badge">Architecture Diff & Dependency Evolution</span>
        <h1 className="hero-title">Architecture Diff</h1>
        <p className="hero-subtitle">
          同一リポジトリの2つのコミット（BASE / HEAD）間の依存グラフを比較し、
          パッケージ・モジュールの追加・削除・変更や循環依存（SCC）の発生・解消を可視化します。
        </p>
      </header>

      <div className="card form-card">
        <form onSubmit={handleSubmit} className="repo-form" noValidate>
          <div className="form-header-row">
            <label htmlFor="repo-url-input" className="field-label">
              GitHub Repository URL (Python & Go)
            </label>
            <span className="badge badge-live-mode">COMMIT GRAPH DIFF</span>
          </div>

          <div style={{ marginBottom: "1rem" }}>
            <input
              id="repo-url-input"
              type="url"
              value={repoUrl}
              onChange={(e) => setRepoUrl(e.target.value)}
              placeholder="https://github.com/owner/repo"
              className="url-input"
              style={{ width: "100%" }}
              disabled={submitting}
              autoFocus
            />
          </div>

          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "12px", marginBottom: "1.2rem" }}>
            <div>
              <label htmlFor="base-sha-input" className="field-label" style={{ marginBottom: "4px", display: "block" }}>
                BASE Commit SHA
              </label>
              <input
                id="base-sha-input"
                type="text"
                value={baseSha}
                onChange={(e) => setBaseSha(e.target.value)}
                placeholder="40文字のコミットSHA (例: 2077efa...)"
                className="url-input"
                style={{ width: "100%", fontFamily: "monospace" }}
                disabled={submitting}
              />
            </div>
            <div>
              <label htmlFor="head-sha-input" className="field-label" style={{ marginBottom: "4px", display: "block" }}>
                HEAD Commit SHA
              </label>
              <input
                id="head-sha-input"
                type="text"
                value={headSha}
                onChange={(e) => setHeadSha(e.target.value)}
                placeholder="40文字のコミットSHA (例: cd37803...)"
                className="url-input"
                style={{ width: "100%", fontFamily: "monospace" }}
                disabled={submitting}
              />
            </div>
          </div>

          <div style={{ display: "flex", justifyContent: "flex-end" }}>
            <button
              type="submit"
              className="btn btn-primary"
              disabled={submitting}
              data-testid="submit-diff-btn"
            >
              {submitting ? "差分解析ジョブ登録中..." : "アーキテクチャ差分を比較"}
            </button>
          </div>

          {errorMessage && (
            <div className="error-banner" role="alert" style={{ marginTop: "1rem" }}>
              <span>{errorMessage}</span>
            </div>
          )}

          <div className="sample-row" style={{ marginTop: "1.2rem" }}>
            <span className="sample-label">サンプル差分を試す:</span>
            {SAMPLE_DIFFS.map((s) => (
              <button
                key={s.title}
                type="button"
                className={`chip-btn ${repoUrl === s.url ? "chip-btn-active" : ""}`}
                onClick={() => applySample(s)}
                disabled={submitting}
                title={s.desc}
              >
                {s.title} <span className="sample-chip-sub">({s.desc})</span>
              </button>
            ))}
          </div>
        </form>

        <div className="demo-cta-bar" style={{ marginTop: "1.5rem" }}>
          <div>
            <div className="demo-cta-heading">
              <span className="badge badge-demo">SINGLE COMMIT ANALYSIS</span>
              <strong>通常のリポジトリ単体探索を行いたい場合:</strong>
            </div>
            <p className="muted-text">
              単一のコミットに対するGuided Code Tour・Impact Analysis・Architecture Viewはトップページから利用できます。
            </p>
          </div>
          <Link href="/" className="btn btn-secondary">
            トップページへ戻る
          </Link>
        </div>
      </div>
    </main>
  );
}
