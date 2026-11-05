"use client";

import React, { useState } from "react";
import type { PRImpactResponse } from "@/lib/types";

interface PRImpactPanelProps {
  impact: PRImpactResponse;
  onSelectNode?: (filePath: string) => void;
  selectedNodeId?: string | null;
}

export function PRImpactPanel({
  impact,
  onSelectNode,
  selectedNodeId,
}: PRImpactPanelProps) {
  const [activeTab, setActiveTab] = useState<
    "changed" | "direct" | "indirect" | "unanalyzed"
  >("changed");

  const {
    pull_number,
    title,
    base_sha,
    head_sha,
    summary,
    changed_files,
    affected_files,
    unanalyzed_files,
  } = impact;

  const directFiles = affected_files.filter((f) => f.depth === 1);
  const indirectFiles = affected_files.filter((f) => f.depth > 1);

  const getStatusBadge = (status: string) => {
    switch (status) {
      case "added":
        return <span className="pr-status-badge pr-status-added">追加 (added)</span>;
      case "modified":
        return <span className="pr-status-badge pr-status-modified">変更 (modified)</span>;
      case "removed":
        return <span className="pr-status-badge pr-status-removed">削除 (removed)</span>;
      case "renamed":
        return <span className="pr-status-badge pr-status-renamed">リネーム (renamed)</span>;
      default:
        return <span className="pr-status-badge pr-status-modified">{status}</span>;
    }
  };

  const getReasonLabel = (reason?: string) => {
    if (!reason) return "解析対象外";
    switch (reason) {
      case "non_python_file":
        return "Python以外のファイル（設定・ドキュメント・他言語等）";
      case "file_removed_in_pr":
        return "PRで削除されたためHEADリポジトリに存在せず、HEADグラフ上の影響は未評価";
      case "unsupported_encoding":
        return "非対応のエンコーディング形式";
      case "file_too_large":
        return "ファイルサイズ上限超過";
      default:
        return reason;
    }
  };

  return (
    <div className="pr-impact-panel" data-testid="pr-impact-panel">
      {/* PR Header */}
      <div className="pr-impact-header">
        <div className="flex items-center justify-between gap-3 mb-2">
          <div className="flex items-center gap-2">
            <span className="text-xs font-mono font-bold px-2 py-0.5 rounded bg-sky-950 text-sky-400 border border-sky-800">
              PR #{pull_number}
            </span>
          </div>
          <span
            className={`text-xs px-2 py-0.5 rounded font-medium ${
              summary.analysis_completeness === "complete"
                ? "bg-emerald-950/60 text-emerald-400 border border-emerald-800/80"
                : "bg-amber-950/60 text-amber-400 border border-amber-800/80"
            }`}
          >
            {summary.analysis_completeness === "complete"
              ? "解析完了 (Complete)"
              : `一部解析 (${summary.analysis_completeness})`}
          </span>
        </div>

        <h2 className="text-base font-bold text-slate-100 mb-2 leading-snug">
          {title}
        </h2>

        <div className="text-xs font-mono text-slate-400 flex flex-wrap items-center gap-x-3 gap-y-1">
          <span>
            BASE: <code className="text-slate-300">{base_sha.slice(0, 7)}</code>
          </span>
          <span>→</span>
          <span>
            HEAD: <code className="text-slate-300">{head_sha.slice(0, 7)}</code>
          </span>
        </div>
      </div>

      {/* Summary KPI Cards */}
      <div className="pr-metric-grid">
        <div className="pr-metric-card">
          <div className="pr-metric-val text-amber-400">
            {summary.changed_files_count}
          </div>
          <div className="pr-metric-label">変更ファイル</div>
        </div>
        <div className="pr-metric-card">
          <div className="pr-metric-val text-rose-400">
            {summary.direct_impact_count}
          </div>
          <div className="pr-metric-label">直接影響 (D1)</div>
        </div>
        <div className="pr-metric-card">
          <div className="pr-metric-val text-orange-400">
            {summary.indirect_impact_count}
          </div>
          <div className="pr-metric-label">間接影響 (D2+)</div>
        </div>
        <div className="pr-metric-card">
          <div className="pr-metric-val text-sky-400">
            {summary.total_affected_count}
          </div>
          <div className="pr-metric-label">影響ファイル総数</div>
        </div>
      </div>

      {/* Navigation Tabs */}
      <div className="pr-impact-tabs">
        <button
          type="button"
          onClick={() => setActiveTab("changed")}
          className={`pr-tab-btn ${
            activeTab === "changed" ? "pr-tab-btn-active" : ""
          }`}
        >
          変更ファイル ({changed_files.length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("direct")}
          className={`pr-tab-btn ${
            activeTab === "direct" ? "pr-tab-btn-active" : ""
          }`}
        >
          直接影響 ({directFiles.length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("indirect")}
          className={`pr-tab-btn ${
            activeTab === "indirect" ? "pr-tab-btn-active" : ""
          }`}
        >
          間接影響 ({indirectFiles.length})
        </button>
        {unanalyzed_files.length > 0 && (
          <button
            type="button"
            onClick={() => setActiveTab("unanalyzed")}
            className={`pr-tab-btn ${
              activeTab === "unanalyzed" ? "pr-tab-btn-active" : ""
            }`}
          >
            未解析 ({unanalyzed_files.length})
          </button>
        )}
      </div>

      {/* Tab Contents */}
      <div className="pr-impact-list">
        {/* Changed Files */}
        {activeTab === "changed" && (
          <div className="space-y-2">
            {changed_files.map((file) => {
              const isSelected = selectedNodeId === file.path;
              return (
                <div
                  key={file.path}
                  onClick={() => file.is_analyzed && onSelectNode?.(file.path)}
                  className={`pr-file-item ${
                    isSelected ? "pr-file-item-selected" : ""
                  } ${
                    file.is_analyzed
                      ? "cursor-pointer hover:bg-slate-800/80"
                      : "opacity-75"
                  }`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span
                      className="font-mono text-xs text-slate-200 font-semibold truncate"
                      title={file.path}
                    >
                      {file.path}
                    </span>
                    {getStatusBadge(file.status)}
                  </div>

                  {file.previous_path && (
                    <div className="text-[11px] text-slate-400 mt-1 font-mono truncate">
                      旧パス: {file.previous_path}
                    </div>
                  )}

                  <div className="flex items-center justify-between mt-1 text-[11px] text-slate-400">
                    <span>
                      {file.is_analyzed ? (
                        <span className="text-emerald-400">● HEAD解析済み</span>
                      ) : (
                        <span className="text-slate-500">
                          ○ {getReasonLabel(file.unanalyzed_reason)}
                        </span>
                      )}
                    </span>
                    <span className="font-mono">
                      <span className="text-emerald-400">+{file.additions}</span> /{" "}
                      <span className="text-rose-400">-{file.deletions}</span>
                    </span>
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {/* Direct Impact Files */}
        {activeTab === "direct" && (
          <div className="space-y-2">
            {directFiles.length === 0 ? (
              <div className="text-center py-8 text-xs text-slate-400">
                変更ファイルに直接依存しているPythonファイルはありません。
              </div>
            ) : (
              directFiles.map((file) => {
                const isSelected = selectedNodeId === file.path;
                return (
                  <div
                    key={file.path}
                    onClick={() => onSelectNode?.(file.path)}
                    className={`pr-file-item cursor-pointer hover:bg-slate-800/80 ${
                      isSelected ? "pr-file-item-selected" : ""
                    }`}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span
                        className="font-mono text-xs text-rose-300 font-semibold truncate"
                        title={file.path}
                      >
                        {file.path}
                      </span>
                      <span className="px-1.5 py-0.5 text-[10px] font-bold rounded bg-rose-950 text-rose-300 border border-rose-800">
                        Depth 1
                      </span>
                    </div>

                    <div className="mt-1.5 text-[11px] text-slate-400">
                      <span className="text-slate-500">依存元変更ファイル: </span>
                      <code className="text-amber-300 font-mono text-[11px]">
                        {file.source_changed_files.join(", ")}
                      </code>
                    </div>

                    {file.representative_dependency_path &&
                      file.representative_dependency_path.length > 0 && (
                        <div className="mt-1 text-[11px] font-mono text-slate-400 bg-slate-900/80 p-1.5 rounded border border-slate-800 overflow-x-auto whitespace-nowrap">
                          <span className="text-slate-500 mr-1">経路:</span>
                          {file.representative_dependency_path.map((step, idx) => (
                            <React.Fragment key={step}>
                              {idx > 0 && (
                                <span className="text-slate-600 mx-1">→</span>
                              )}
                              <span
                                className={
                                  idx === 0
                                    ? "text-rose-300 font-medium"
                                    : idx ===
                                      file.representative_dependency_path.length - 1
                                    ? "text-amber-300 font-medium"
                                    : "text-slate-300"
                                }
                              >
                                {step.split("/").pop()}
                              </span>
                            </React.Fragment>
                          ))}
                        </div>
                      )}
                  </div>
                );
              })
            )}
          </div>
        )}

        {/* Indirect Impact Files */}
        {activeTab === "indirect" && (
          <div className="space-y-2">
            {indirectFiles.length === 0 ? (
              <div className="text-center py-8 text-xs text-slate-400">
                間接的に影響を受けるPythonファイルはありません。
              </div>
            ) : (
              indirectFiles.map((file) => {
                const isSelected = selectedNodeId === file.path;
                return (
                  <div
                    key={file.path}
                    onClick={() => onSelectNode?.(file.path)}
                    className={`pr-file-item cursor-pointer hover:bg-slate-800/80 ${
                      isSelected ? "pr-file-item-selected" : ""
                    }`}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span
                        className="font-mono text-xs text-orange-300 font-semibold truncate"
                        title={file.path}
                      >
                        {file.path}
                      </span>
                      <span className="px-1.5 py-0.5 text-[10px] font-bold rounded bg-orange-950 text-orange-300 border border-orange-800">
                        Depth {file.depth}
                      </span>
                    </div>

                    <div className="mt-1.5 text-[11px] text-slate-400">
                      <span className="text-slate-500">波及起点変更: </span>
                      <code className="text-amber-300 font-mono text-[11px]">
                        {file.source_changed_files.join(", ")}
                      </code>
                    </div>

                    {file.representative_dependency_path &&
                      file.representative_dependency_path.length > 0 && (
                        <div className="mt-1 text-[11px] font-mono text-slate-400 bg-slate-900/80 p-1.5 rounded border border-slate-800 overflow-x-auto whitespace-nowrap">
                          <span className="text-slate-500 mr-1">最短経路:</span>
                          {file.representative_dependency_path.map((step, idx) => (
                            <React.Fragment key={step}>
                              {idx > 0 && (
                                <span className="text-slate-600 mx-1">→</span>
                              )}
                              <span
                                className={
                                  idx === 0
                                    ? "text-orange-300 font-medium"
                                    : idx ===
                                      file.representative_dependency_path.length - 1
                                    ? "text-amber-300 font-medium"
                                    : "text-slate-300"
                                }
                              >
                                {step.split("/").pop()}
                              </span>
                            </React.Fragment>
                          ))}
                        </div>
                      )}
                  </div>
                );
              })
            )}
          </div>
        )}

        {/* Unanalyzed Files */}
        {activeTab === "unanalyzed" && (
          <div className="space-y-2">
            {unanalyzed_files.map((file) => (
              <div key={file.path} className="pr-file-item opacity-85">
                <div className="flex items-center justify-between gap-2">
                  <span
                    className="font-mono text-xs text-slate-300 font-semibold truncate"
                    title={file.path}
                  >
                    {file.path}
                  </span>
                  <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-slate-800 text-slate-400 border border-slate-700">
                    {file.unanalyzed_reason || "対象外"}
                  </span>
                </div>
                <div className="mt-1.5 text-xs text-slate-400">
                  {getReasonLabel(file.unanalyzed_reason)}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
