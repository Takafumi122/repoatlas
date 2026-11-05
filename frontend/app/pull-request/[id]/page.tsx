"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import {
  APIClientError,
  getAnalysisGraph,
  getPRAnalysisImpact,
  getPRAnalysisStatus,
} from "@/lib/api";
import type {
  GraphResponse,
  PRAnalysisStatusResponse,
  PRImpactResponse,
} from "@/lib/types";
import { RepositoryGraph } from "@/components/RepositoryGraph";
import { PRImpactPanel } from "@/components/PRImpactPanel";

const POLL_INTERVAL_MS = 1500;

export default function PRAnalysisDetailPage() {
  const params = useParams<{ id: string }>();
  const prAnalysisId = params?.id || "";

  const [statusData, setStatusData] =
    useState<PRAnalysisStatusResponse | null>(null);
  const [impactData, setImpactData] = useState<PRImpactResponse | null>(null);
  const [graphData, setGraphData] = useState<GraphResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pollTick, setPollTick] = useState(0);

  const handleRetry = useCallback(() => {
    setError(null);
    setImpactData(null);
    setGraphData(null);
    setPollTick((t) => t + 1);
  }, []);

  useEffect(() => {
    if (!prAnalysisId) return;

    let cancelled = false;
    let timerId: ReturnType<typeof setTimeout> | null = null;
    const abortCtrl = new AbortController();

    async function poll() {
      try {
        const status = await getPRAnalysisStatus(
          prAnalysisId,
          abortCtrl.signal
        );
        if (cancelled) return;
        setStatusData(status);

        if (status.status === "completed") {
          const impact = await getPRAnalysisImpact(
            prAnalysisId,
            abortCtrl.signal
          );
          if (cancelled) return;
          setImpactData(impact);

          const headId = impact.head_analysis_id || status.head_analysis_id;
          if (headId) {
            const graph = await getAnalysisGraph(
              headId,
              abortCtrl.signal
            );
            if (cancelled) return;
            setGraphData(graph);
          }
          return;
        }

        if (status.status === "failed") {
          setError(
            status.error?.message ||
              "PR解析処理中にエラーが発生しました。"
          );
          return;
        }

        timerId = setTimeout(poll, POLL_INTERVAL_MS);
      } catch (err) {
        if (cancelled || abortCtrl.signal.aborted) return;
        if (err instanceof APIClientError) {
          setError(err.message);
        } else {
          setError("PR解析状態の取得に失敗しました。");
        }
      }
    }

    poll();

    return () => {
      cancelled = true;
      abortCtrl.abort();
      if (timerId) {
        clearTimeout(timerId);
      }
    };
  }, [prAnalysisId, pollTick]);

  if (impactData && graphData) {
    const ghRepoUrl = `https://github.com/${impactData.repository.owner}/${impactData.repository.name}/pull/${impactData.pull_number}`;

    return (
      <div className="pr-analysis-layout flex flex-col h-screen overflow-hidden bg-slate-950">
        <header className="pr-top-bar px-4 py-2 bg-slate-900 border-b border-slate-800 flex items-center justify-between z-20">
          <div className="flex items-center gap-3">
            <Link
              href="/"
              className="text-sky-400 hover:text-sky-300 font-bold text-sm flex items-center gap-1.5 transition-colors"
            >
              <span>←</span> RepoAtlas
            </Link>
            <span className="text-slate-600">|</span>
            <span className="text-xs text-slate-300 font-medium">
              PR #{impactData.pull_number}: {impactData.title}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <a
              href={ghRepoUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="text-xs text-slate-400 hover:text-slate-200 transition-colors"
            >
              GitHubで開く ↗
            </a>
          </div>
        </header>

        <div className="pr-split-container flex-1 flex flex-col md:flex-row overflow-hidden">
          <aside className="pr-sidebar w-full md:w-[380px] lg:w-[440px] border-r border-slate-800 bg-slate-900/60 overflow-y-auto">
            <PRImpactPanel impact={impactData} />
          </aside>
          <main className="pr-graph-area flex-1 relative h-full">
            <RepositoryGraph graph={graphData} prImpact={impactData} />
          </main>
        </div>
      </div>
    );
  }

  // Loading / Progress state
  const stageLabels: Record<string, string> = {
    pending: "解析キュー待機中...",
    resolving_metadata: "GitHub PRメタデータ・変更ファイル取得中...",
    analyzing_head: "HEADコミットのPython依存グラフ解析中...",
    analyzing_impact: "変更ファイルを起点としたMulti-source BFS逆方向影響解析中...",
    persisting: "解析エビデンスおよび影響範囲をデータベースに永続化中...",
  };

  return (
    <main className="progress-page flex items-center justify-center min-h-screen bg-slate-950 p-4">
      <div className="card max-w-md w-full p-6 text-center">
        <h2 className="text-lg font-bold text-slate-100 mb-2">
          Pull Request 影響解析
        </h2>
        <p className="text-xs text-slate-400 mb-6">
          PR #{statusData?.pull_number || ""}
        </p>

        {error ? (
          <div className="space-y-4">
            <div className="p-3 bg-red-950/50 border border-red-800/80 rounded-lg text-red-200 text-xs">
              {error}
            </div>
            <button
              onClick={handleRetry}
              className="btn btn-primary w-full text-xs"
            >
              再試行する
            </button>
            <div>
              <Link href="/" className="text-xs text-slate-400 hover:text-slate-200">
                トップに戻る
              </Link>
            </div>
          </div>
        ) : (
          <div className="space-y-4">
            <div className="w-10 h-10 border-2 border-sky-500 border-t-transparent rounded-full animate-spin mx-auto" />
            <p className="text-xs text-sky-400 font-medium">
              {stageLabels[statusData?.stage || ""] ||
                "PR変更ファイルと依存影響を分析しています..."}
            </p>
            <p className="text-[11px] text-slate-500">
              公開リポジトリのPRメタデータ、HEADコミットのAST解析、逆方向BFSを自動実行しています。
            </p>
          </div>
        )}
      </div>
    </main>
  );
}
