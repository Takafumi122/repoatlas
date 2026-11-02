"use client";

import React from "react";
import Link from "next/link";
import type {
  GraphNodeData,
  GraphResponse,
  GraphViewMode,
} from "@/lib/types";

interface GraphToolbarProps {
  graph: GraphResponse;
  visibleNodeCount: number;
  hiddenByLimitCount: number;
  directories: string[];
  selectedDirectory: string;
  onDirectoryChange: (dir: string) => void;
  searchQuery: string;
  onSearchChange: (q: string) => void;
  selectedNodeId: string | null;
  onSelectNode: (nodeId: string | null) => void;
  viewMode: GraphViewMode;
  onViewModeChange: (mode: GraphViewMode) => void;
  architectureNodeCount: number;
}

export function GraphToolbar({
  graph,
  visibleNodeCount,
  hiddenByLimitCount,
  directories,
  selectedDirectory,
  onDirectoryChange,
  searchQuery,
  onSearchChange,
  selectedNodeId,
  onSelectNode,
  viewMode,
  onViewModeChange,
  architectureNodeCount,
}: GraphToolbarProps) {
  const shortSha = graph.commit_sha ? graph.commit_sha.slice(0, 7) : "unknown";
  const nodeById = new Map<string, GraphNodeData>(
    graph.nodes.map((n) => [n.id, n])
  );

  return (
    <header className="graph-toolbar">
      <div className="toolbar-top">
        <div className="toolbar-repo-info">
          <Link href="/" className="brand-mini">
            RepoAtlas
          </Link>
          {graph.demo && (
            <span className="badge badge-demo" data-testid="demo-badge">
              DEMO DATASET (同梱サンプル)
            </span>
          )}
          <a
            href={graph.repository.html_url}
            target="_blank"
            rel="noopener noreferrer"
            className="repo-title-link"
          >
            {graph.repository.owner}/{graph.repository.name}
          </a>
          <span className="sha-pill" title={`Commit SHA: ${graph.commit_sha}`}>
            SHA: <code>{shortSha}</code>
          </span>
        </div>

        <div className="toolbar-metrics">
          <div className="view-mode-switch" role="group" aria-label="グラフ表示モード切替">
            <button
              type="button"
              className={`chip-btn ${
                viewMode === "file" ? "chip-btn-active" : ""
              }`}
              onClick={() => onViewModeChange("file")}
              data-testid="view-mode-file"
            >
              File View ({graph.nodes.length})
            </button>
            <button
              type="button"
              className={`chip-btn ${
                viewMode === "architecture" ? "chip-btn-active" : ""
              }`}
              onClick={() => onViewModeChange("architecture")}
              data-testid="view-mode-architecture"
            >
              Architecture View ({architectureNodeCount})
            </button>
          </div>

          <span className="metric-pill">
            解析ファイル: <strong>{graph.summary.files}</strong>
          </span>
          <span className="metric-pill">
            描画中: <strong>{visibleNodeCount}</strong> / {graph.nodes.length}
          </span>
          <span className="metric-pill">
            内部エッジ: <strong>{graph.summary.internal_edges}</strong>
          </span>
          <span className="metric-pill">
            外部import: <strong>{graph.summary.external_imports}</strong>
          </span>
          <span className="metric-pill">
            未解決: <strong>{graph.summary.unresolved_imports}</strong>
          </span>
        </div>
      </div>

      {hiddenByLimitCount > 0 && viewMode === "file" && (
        <div className="limit-warning-bar" role="status">
          大規模リポジトリのため重要度上位300ノードのみを描画しています（非表示:{" "}
          <strong>{hiddenByLimitCount}</strong>
          件）。ディレクトリフィルタや検索、またはArchitecture Viewで全体を把握できます。
        </div>
      )}

      <div className="toolbar-controls">
        <div className="filter-group">
          <input
            type="search"
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="ファイル名・パスで検索..."
            className="search-input"
            aria-label="ファイルパス検索"
          />
          <select
            value={selectedDirectory}
            onChange={(e) => onDirectoryChange(e.target.value)}
            className="dir-select"
            aria-label="ディレクトリフィルタ"
          >
            <option value="ALL">
              すべてのディレクトリ ({graph.nodes.length})
            </option>
            {directories.map((dir) => (
              <option key={dir} value={dir}>
                {dir}
              </option>
            ))}
          </select>
          {(searchQuery || selectedDirectory !== "ALL" || selectedNodeId) && (
            <button
              type="button"
              className="chip-btn"
              onClick={() => {
                onSearchChange("");
                onDirectoryChange("ALL");
                onSelectNode(null);
              }}
            >
              リセット
            </button>
          )}
        </div>

        <div className="recommended-bar">
          <span className="rec-bar-label">推奨閲覧順 Top 5:</span>
          {graph.recommended_file_ids.map((id, index) => {
            const node = nodeById.get(id);
            if (!node) return null;
            const active = selectedNodeId === id;
            return (
              <button
                key={id}
                type="button"
                className={`chip-btn ${active ? "chip-btn-active" : ""}`}
                onClick={() => {
                  if (viewMode !== "file") {
                    onViewModeChange("file");
                  }
                  onSelectNode(id);
                }}
                title={`重要度スコア: ${node.importance_score.toFixed(1)} (被依存:${node.in_degree}, 依存先:${node.out_degree}${node.is_entrypoint ? ", エントリーポイント" : ""})`}
              >
                #{index + 1} {node.path}
              </button>
            );
          })}
        </div>
      </div>
    </header>
  );
}
