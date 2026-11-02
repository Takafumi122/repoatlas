"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import {
  APIClientError,
  getAnalysisGraph,
  getAnalysisStatus,
} from "@/lib/api";
import type { AnalysisStatusResponse, GraphResponse } from "@/lib/types";
import { AnalysisProgress } from "@/components/AnalysisProgress";
import { RepositoryGraph } from "@/components/RepositoryGraph";

const POLL_INTERVAL_MS = 1500;

export default function AnalysisDetailPage() {
  const params = useParams<{ id: string }>();
  const analysisId = params?.id || "";

  const [statusData, setStatusData] = useState<AnalysisStatusResponse | null>(
    null
  );
  const [graphData, setGraphData] = useState<GraphResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pollTick, setPollTick] = useState(0);

  const handleRetry = useCallback(() => {
    setError(null);
    setGraphData(null);
    setPollTick((t) => t + 1);
  }, []);

  useEffect(() => {
    if (!analysisId) return;

    let cancelled = false;
    let timerId: ReturnType<typeof setTimeout> | null = null;
    const abortCtrl = new AbortController();

    async function poll() {
      try {
        const status = await getAnalysisStatus(analysisId, abortCtrl.signal);
        if (cancelled) return;
        setStatusData(status);

        if (status.status === "completed") {
          const graph = await getAnalysisGraph(analysisId, abortCtrl.signal);
          if (cancelled) return;
          setGraphData(graph);
          return;
        }

        if (status.status === "failed") {
          setError(
            status.error?.message || "解析処理中にエラーが発生しました。"
          );
          return;
        }

        timerId = setTimeout(poll, POLL_INTERVAL_MS);
      } catch (err) {
        if (cancelled || abortCtrl.signal.aborted) return;
        if (err instanceof APIClientError) {
          setError(err.message);
        } else {
          setError("解析状態の取得に失敗しました。");
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
  }, [analysisId, pollTick]);

  if (graphData) {
    return <RepositoryGraph graph={graphData} />;
  }

  return (
    <main className="progress-page">
      <AnalysisProgress
        statusData={statusData}
        error={error}
        onRetry={handleRetry}
      />
    </main>
  );
}
