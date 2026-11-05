"use client";

import React from "react";
import type { DiffSummary, CycleChanges } from "@/lib/types";

export type DiffFilterType = "all" | "added" | "removed" | "modified" | "cyclic";
export type DiffViewMode = "diff" | "base" | "head";

interface DiffSummaryBannerProps {
  summary: DiffSummary;
  cycleChanges: CycleChanges;
  viewMode: DiffViewMode;
  onViewModeChange: (mode: DiffViewMode) => void;
  filter: DiffFilterType;
  onFilterChange: (filter: DiffFilterType) => void;
}

export function DiffSummaryBanner({
  summary,
  cycleChanges,
  viewMode,
  onViewModeChange,
  filter,
  onFilterChange,
}: DiffSummaryBannerProps) {
  const shortBase = summary.base_commit_sha.slice(0, 7);
  const shortHead = summary.head_commit_sha.slice(0, 7);

  const addedCyclesCount = cycleChanges.added_cycles ? cycleChanges.added_cycles.length : 0;
  const resolvedCyclesCount = cycleChanges.resolved_cycles ? cycleChanges.resolved_cycles.length : 0;

  return (
    <div>
      <div className="diff-summary-banner" data-testid="diff-summary-banner">
        <div className="diff-stat-group">
          <span style={{ fontWeight: 700, color: "var(--text-main)" }}>
            Architecture Diff:
          </span>
          <code style={{ fontSize: "0.85rem", color: "var(--accent-focus)" }}>
            {shortBase}
          </code>
          <span>→</span>
          <code style={{ fontSize: "0.85rem", color: "var(--accent-focus)" }}>
            {shortHead}
          </code>
          <span className="badge badge-package" style={{ marginLeft: "4px" }}>
            {summary.language.toUpperCase()} ({summary.granularity})
          </span>
        </div>

        <div className="diff-stat-group" data-testid="diff-counts-group">
          <span className="diff-metric-chip badge-diff-added" title="追加されたノード">
            +{summary.added_nodes_count} ノード
          </span>
          <span className="diff-metric-chip badge-diff-removed" title="削除されたノード">
            -{summary.removed_nodes_count} ノード
          </span>
          <span className="diff-metric-chip badge-diff-modified" title="変更されたノード">
            ~{summary.modified_nodes_count} 変更
          </span>
          <span className="diff-metric-chip badge-diff-added" title="追加されたエッジ">
            +{summary.added_edges_count} 依存
          </span>
          <span className="diff-metric-chip badge-diff-removed" title="削除されたエッジ">
            -{summary.removed_edges_count} 依存
          </span>

          {addedCyclesCount > 0 && (
            <span
              className="diff-metric-chip badge-diff-cycle"
              title="HEADで新しく発生した循環依存"
              data-testid="badge-added-cycles"
            >
              ⚠️ +{addedCyclesCount} 循環依存発生
            </span>
          )}
          {resolvedCyclesCount > 0 && (
            <span
              className="diff-metric-chip badge-diff-added"
              title="HEADで解消された循環依存"
              data-testid="badge-resolved-cycles"
            >
              ✅ {resolvedCyclesCount} 循環解消
            </span>
          )}
        </div>

        <div className="diff-view-segmented" data-testid="diff-view-toggle">
          <button
            type="button"
            className={`diff-seg-btn ${viewMode === "diff" ? "diff-seg-btn-active" : ""}`}
            onClick={() => onViewModeChange("diff")}
            data-testid="view-mode-diff"
          >
            Diff View
          </button>
          <button
            type="button"
            className={`diff-seg-btn ${viewMode === "base" ? "diff-seg-btn-active" : ""}`}
            onClick={() => onViewModeChange("base")}
            data-testid="view-mode-base"
          >
            BASE ({shortBase})
          </button>
          <button
            type="button"
            className={`diff-seg-btn ${viewMode === "head" ? "diff-seg-btn-active" : ""}`}
            onClick={() => onViewModeChange("head")}
            data-testid="view-mode-head"
          >
            HEAD ({shortHead})
          </button>
        </div>
      </div>

      <div className="diff-filter-bar" data-testid="diff-filter-bar">
        <span style={{ color: "var(--text-muted)", marginRight: "4px" }}>
          フィルター:
        </span>
        <button
          type="button"
          className={`chip-btn ${filter === "all" ? "chip-btn-active" : ""}`}
          onClick={() => onFilterChange("all")}
          data-testid="filter-all"
        >
          すべて表示
        </button>
        <button
          type="button"
          className={`chip-btn ${filter === "added" ? "chip-btn-active" : ""}`}
          onClick={() => onFilterChange("added")}
          data-testid="filter-added"
        >
          追加のみ (+{summary.added_nodes_count})
        </button>
        <button
          type="button"
          className={`chip-btn ${filter === "removed" ? "chip-btn-active" : ""}`}
          onClick={() => onFilterChange("removed")}
          data-testid="filter-removed"
        >
          削除のみ (-{summary.removed_nodes_count})
        </button>
        <button
          type="button"
          className={`chip-btn ${filter === "modified" ? "chip-btn-active" : ""}`}
          onClick={() => onFilterChange("modified")}
          data-testid="filter-modified"
        >
          変更のみ (~{summary.modified_nodes_count})
        </button>
        <button
          type="button"
          className={`chip-btn ${filter === "cyclic" ? "chip-btn-active" : ""}`}
          onClick={() => onFilterChange("cyclic")}
          data-testid="filter-cyclic"
        >
          循環依存のみ ({addedCyclesCount + resolvedCyclesCount}件)
        </button>
      </div>
    </div>
  );
}
