"use client";

import React, { useEffect, useState } from "react";
import { getAnalysisEvidence } from "@/lib/api";
import type { ImportEvidence } from "@/lib/types";

interface EdgeEvidenceDrawerProps {
  analysisId: string;
  sourcePath: string;
  targetPath: string;
  repoOwner?: string;
  repoName?: string;
  commitSha?: string;
  onClose: () => void;
}

export function EdgeEvidenceDrawer({
  analysisId,
  sourcePath,
  targetPath,
  repoOwner,
  repoName,
  commitSha,
  onClose,
}: EdgeEvidenceDrawerProps) {
  const [evidences, setEvidences] = useState<ImportEvidence[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);

    getAnalysisEvidence(analysisId, { sourcePath, targetPath })
      .then((res) => {
        if (active) {
          setEvidences(res.evidences || []);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (active) {
          setError(err instanceof Error ? err.message : "エビデンスの取得に失敗しました");
          setLoading(false);
        }
      });

    return () => {
      active = false;
    };
  }, [analysisId, sourcePath, targetPath]);

  return (
    <div className="evidence-drawer" data-testid="edge-evidence-drawer">
      <div className="evidence-header">
        <div>
          <div className="text-xs text-sky-400 font-semibold tracking-wide uppercase">
            Dependency Evidence（依存関係の根拠）
          </div>
          <div className="text-sm font-bold text-slate-100 truncate mt-0.5" title={`${sourcePath} → ${targetPath}`}>
            {sourcePath} <span className="text-slate-400">→</span> {targetPath}
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-1.5 text-slate-400 hover:text-slate-200 hover:bg-slate-800 rounded-lg transition-colors text-sm font-bold"
          aria-label="Close Evidence Drawer"
        >
          ✕
        </button>
      </div>

      <div className="evidence-content">
        <div className="text-xs text-slate-400 bg-slate-900/60 p-2.5 rounded-lg border border-slate-800">
          この依存エッジは、以下のAST静的解析で抽出された厳密なimport文に基づいています。
        </div>

        {loading && (
          <div className="text-center py-8 text-slate-400 text-sm animate-pulse">
            エビデンスを照会中...
          </div>
        )}

        {error && (
          <div className="p-3 bg-red-950/40 border border-red-800/60 text-red-300 text-xs rounded-lg">
            {error}
          </div>
        )}

        {!loading && !error && evidences.length === 0 && (
          <div className="text-center py-8 text-slate-400 text-sm">
            該当するimportエビデンスが見つかりませんでした。
          </div>
        )}

        {!loading &&
          !error &&
          evidences.map((ev) => {
            const githubUrl =
              repoOwner && repoName && commitSha
                ? `https://github.com/${repoOwner}/${repoName}/blob/${commitSha}/${ev.source_path}#L${ev.start_line}`
                : null;

            return (
              <div key={ev.id} className="evidence-card" data-testid={`evidence-card-${ev.id}`}>
                <div className="flex items-center justify-between text-xs">
                  <span className="font-semibold text-slate-300">
                    L{ev.start_line}:{ev.start_column} → L{ev.end_line}:{ev.end_column}
                  </span>
                  <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-sky-950/60 border border-sky-800/60 text-sky-300">
                    {ev.import_kind.toUpperCase()}
                  </span>
                </div>

                <pre className="evidence-code-block">{ev.import_statement}</pre>

                {ev.resolution_reason && (
                  <div className="text-[11px] text-slate-400">
                    <span className="text-slate-500 font-semibold">解決理由: </span>
                    {ev.resolution_reason}
                  </div>
                )}

                {githubUrl && (
                  <div className="pt-1 flex justify-end">
                    <a
                      href={githubUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="github-permalink-btn"
                    >
                      <span>GitHubで該当行を表示</span>
                      <span>↗</span>
                    </a>
                  </div>
                )}
              </div>
            );
          })}
      </div>
    </div>
  );
}
