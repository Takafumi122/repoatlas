"use client";

import React, { useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { APIClientError, createAnalysis } from "@/lib/api";

const SAMPLE_REPOS = [
  {
    label: "pallets/itsdangerous",
    desc: "軽量・15ファイル（数秒で完了）",
    url: "https://github.com/pallets/itsdangerous",
  },
  {
    label: "psf/requests",
    desc: "定番HTTPライブラリ・37ファイル",
    url: "https://github.com/psf/requests",
  },
  {
    label: "pallets/click",
    desc: "CLIツールキット",
    url: "https://github.com/pallets/click",
  },
];

export function RepoUrlForm() {
  const router = useRouter();
  const [url, setUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = url.trim();
    if (!trimmed) {
      setErrorMessage("GitHubの公開リポジトリURLを入力してください");
      return;
    }

    setSubmitting(true);
    setErrorMessage(null);

    try {
      const res = await createAnalysis(trimmed);
      router.push(`/analysis/${encodeURIComponent(res.analysis_id)}`);
    } catch (err) {
      if (err instanceof APIClientError) {
        setErrorMessage(err.message);
      } else {
        setErrorMessage(
          "APIサーバーへの接続に失敗しました。下の「デモを開く」から同梱サンプルグラフもお試しいただけます。"
        );
      }
      setSubmitting(false);
    }
  }

  return (
    <div className="card form-card">
      <form onSubmit={handleSubmit} className="repo-form" noValidate>
        <div className="form-header-row">
          <label htmlFor="repository-url-input" className="field-label">
            GitHub Public Repository URL (Python)
          </label>
          <span className="badge badge-live-mode">LIVE GITHUB ANALYSIS</span>
        </div>

        <div className="input-row">
          <input
            id="repository-url-input"
            type="url"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="https://github.com/owner/repo (例: https://github.com/psf/requests)"
            className="url-input"
            disabled={submitting}
            autoFocus
          />
          <button
            type="submit"
            className="btn btn-primary"
            disabled={submitting}
          >
            {submitting ? "解析ジョブ登録中..." : "依存グラフを解析"}
          </button>
        </div>

        {errorMessage && (
          <div className="error-banner" role="alert">
            <span>{errorMessage}</span>
          </div>
        )}

        <div className="sample-row">
          <span className="sample-label">サンプルリポジトリを試す:</span>
          {SAMPLE_REPOS.map((s) => (
            <button
              key={s.url}
              type="button"
              className={`chip-btn ${url === s.url ? "chip-btn-active" : ""}`}
              onClick={() => {
                setUrl(s.url);
                setErrorMessage(null);
              }}
              disabled={submitting}
              title={s.desc}
            >
              {s.label}{" "}
              <span className="sample-chip-sub">({s.desc})</span>
            </button>
          ))}
        </div>
      </form>

      <div className="demo-cta-bar">
        <div>
          <div className="demo-cta-heading">
            <span className="badge badge-demo">INSTANT DEMO</span>
            <strong>待たずにすぐグラフ操作を体験したい場合:</strong>
          </div>
          <p className="muted-text">
            外部GitHub通信なしで、同梱のサンプルPython Webアプリ（7ファイル・11エッジ）のGuided
            Code Tour・Impact Analysis・Architecture Viewを即座に操作できます。
          </p>
        </div>
        <Link href="/demo" className="btn btn-secondary">
          デモを開く
        </Link>
      </div>
    </div>
  );
}
