"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import {
  APIClientError,
  getArchitectureDiffGraph,
  getArchitectureDiffStatus,
} from "@/lib/api";
import type {
  ArchitectureDiffGraphResponse,
  ArchitectureDiffStatusResponse,
} from "@/lib/types";
import { ArchitectureDiffCanvas } from "@/components/ArchitectureDiffCanvas";

const POLL_INTERVAL_MS = 1500;

export default function DiffDetailPage() {
  const params = useParams<{ id: string }>();
  const diffId = params?.id || "";

  const [statusData, setStatusData] =
    useState<ArchitectureDiffStatusResponse | null>(null);
  const [diffGraph, setDiffGraph] =
    useState<ArchitectureDiffGraphResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pollTick, setPollTick] = useState(0);

  const handleRetry = useCallback(() => {
    setError(null);
    setDiffGraph(null);
    setPollTick((t) => t + 1);
  }, []);

  useEffect(() => {
    if (!diffId) return;

    let cancelled = false;
    let timerId: ReturnType<typeof setTimeout> | null = null;
    const abortCtrl = new AbortController();

    async function poll() {
      try {
        const status = await getArchitectureDiffStatus(
          diffId,
          abortCtrl.signal
        );
        if (cancelled) return;
        setStatusData(status);

        if (status.status === "completed") {
          const graph = await getArchitectureDiffGraph(
            diffId,
            abortCtrl.signal
          );
          if (cancelled) return;
          setDiffGraph(graph);
          return;
        }

        if (status.status === "failed") {
          setError(
            status.error?.message ||
              "アーキテクチャ差分解析中にエラーが発生しました。"
          );
          return;
        }

        timerId = setTimeout(poll, POLL_INTERVAL_MS);
      } catch (err) {
        if (cancelled || abortCtrl.signal.aborted) return;
        if (err instanceof APIClientError) {
          setError(err.message);
        } else {
          setError("差分解析状態の取得に失敗しました。");
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
  }, [diffId, pollTick]);

  if (diffGraph) {
    return <ArchitectureDiffCanvas diffData={diffGraph} />;
  }

  return (
    <main className="progress-page">
      <div className="card progress-card" data-testid="diff-progress-card">
        <h2>Architecture Diff 解析中</h2>
        <p className="muted-text">
          BASEコミットおよびHEADコミットのスナップショット解析とグラフ差分を算出しています...
        </p>

        {statusData && (
          <div style={{ margin: "1rem 0", fontSize: "0.9rem" }}>
            <div>
              <strong>リポジトリ:</strong> {statusData.repository.owner}/
              {statusData.repository.name}
            </div>
            <div>
              <strong>BASE SHA:</strong>{" "}
              <code>{statusData.base_sha?.slice(0, 7)}</code> ➔{" "}
              <strong>HEAD SHA:</strong>{" "}
              <code>{statusData.head_sha?.slice(0, 7)}</code>
            </div>
            <div style={{ marginTop: "4px" }}>
              <strong>ステータス:</strong>{" "}
              <span className="badge badge-tour">{statusData.status}</span> (
              {statusData.stage})
            </div>
          </div>
        )}

        {error && (
          <div className="error-banner" role="alert" style={{ margin: "1rem 0" }}>
            <span>{error}</span>
          </div>
        )}

        <div style={{ display: "flex", gap: "10px", marginTop: "1rem" }}>
          {error && (
            <button
              type="button"
              className="btn btn-primary"
              onClick={handleRetry}
            >
              再試行
            </button>
          )}
          <Link href="/diff" className="btn btn-secondary">
            Diff入力画面へ戻る
          </Link>
        </div>
      </div>
    </main>
  );
}
