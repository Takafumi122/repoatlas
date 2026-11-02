"use client";

import React from "react";
import Link from "next/link";
import type { AnalysisStage, AnalysisStatusResponse } from "@/lib/types";

const STAGE_ORDER: { key: AnalysisStage; label: string; desc: string }[] = [
  { key: "queued", label: "Queued", desc: "ワーカーのジョブ取得待ち" },
  { key: "fetching", label: "Fetching", desc: "固定コミットSHAのZIPアーカイブを取得中" },
  { key: "indexing", label: "Indexing", desc: "Pythonファイルとパッケージ構造を索引化中" },
  { key: "parsing", label: "Parsing", desc: "Tree-sitterで構文木・import・関数・クラスを抽出中" },
  { key: "resolving", label: "Resolving", desc: "内部ファイル依存関係と重要度スコアを解決中" },
  { key: "persisting", label: "Persisting", desc: "PostgreSQLに解析結果を原子的保存中" },
  { key: "completed", label: "Completed", desc: "解析完了・グラフ描画準備完了" },
];

interface AnalysisProgressProps {
  statusData: AnalysisStatusResponse | null;
  error: string | null;
  onRetry?: () => void;
}

export function AnalysisProgress({
  statusData,
  error,
  onRetry,
}: AnalysisProgressProps) {
  const isFailed = statusData?.status === "failed" || Boolean(error);
  const currentStage = statusData?.stage || "queued";
  const currentIdx = STAGE_ORDER.findIndex((s) => s.key === currentStage);

  return (
    <div className="card progress-card">
      <div className="progress-header">
        <div>
          <span className="badge badge-status">
            {isFailed ? "FAILED" : statusData?.status?.toUpperCase() || "LOADING"}
          </span>
          <h2 className="progress-title">
            {statusData?.repository
              ? `${statusData.repository.owner}/${statusData.repository.name}`
              : "リポジトリ解析ジョブ"}
          </h2>
          {statusData?.commit_sha && (
            <p className="commit-meta">
              Target Commit SHA: <code>{statusData.commit_sha}</code>
            </p>
          )}
        </div>
      </div>

      {isFailed ? (
        <div className="error-panel" role="alert">
          <h3>解析に失敗しました</h3>
          <p>
            {statusData?.error?.message ||
              error ||
              "解析処理中にエラーが発生しました。時間をおいて再試行してください。"}
          </p>
          {statusData?.error?.code && (
            <p className="muted-text">Error Code: <code>{statusData.error.code}</code></p>
          )}
          <div className="action-row">
            {onRetry && (
              <button type="button" className="btn btn-primary" onClick={onRetry}>
                再試行する
              </button>
            )}
            <Link href="/" className="btn btn-secondary">
              トップへ戻る
            </Link>
            <Link href="/demo" className="btn btn-secondary">
              デモグラフを見る
            </Link>
          </div>
        </div>
      ) : (
        <div className="stages-list" aria-live="polite">
          {STAGE_ORDER.map((step, idx) => {
            let stateClass = "stage-pending";
            if (currentIdx > idx || currentStage === "completed") {
              stateClass = "stage-done";
            } else if (currentIdx === idx) {
              stateClass = "stage-active";
            }

            return (
              <div key={step.key} className={`stage-item ${stateClass}`}>
                <div className="stage-dot" />
                <div className="stage-body">
                  <div className="stage-name">{step.label}</div>
                  <div className="stage-desc">{step.desc}</div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
