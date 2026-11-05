"use client";

import React from "react";
import type {
  DiffNodeData,
  DiffEdgeData,
  DiffSummary,
  CycleChanges,
} from "@/lib/types";

interface DiffDetailsPanelProps {
  selectedNode: DiffNodeData | null;
  selectedEdge: DiffEdgeData | null;
  summary: DiffSummary;
  cycleChanges: CycleChanges;
  onSelectNode: (id: string) => void;
}

export function DiffDetailsPanel({
  selectedNode,
  selectedEdge,
  summary,
  cycleChanges,
  onSelectNode,
}: DiffDetailsPanelProps) {
  // If an edge is selected
  if (selectedEdge) {
    return (
      <aside className="details-panel" aria-label="Diff詳細パネル" data-testid="diff-edge-details">
        <div className="details-header">
          <div className="details-badges">
            <span
              className={`badge ${
                selectedEdge.change_type === "added"
                  ? "badge-diff-added"
                  : selectedEdge.change_type === "removed"
                  ? "badge-diff-removed"
                  : "badge-diff-unchanged"
              }`}
            >
              {selectedEdge.change_type === "added"
                ? "+ 追加された依存"
                : selectedEdge.change_type === "removed"
                ? "- 削除された依存"
                : "= 維持された依存"}
            </span>
            {selectedEdge.is_cyclic && (
              <span className="badge badge-diff-cycle">
                {selectedEdge.cycle_change_type === "added"
                  ? "⚠️ 循環依存発生 (Added Cycle)"
                  : selectedEdge.cycle_change_type === "resolved"
                  ? "✅ 循環依存解消 (Resolved Cycle)"
                  : "循環依存 (Cycle)"}
              </span>
            )}
          </div>
          <h3 className="details-filepath">依存関係の詳細</h3>
        </div>

        <div className="details-section">
          <h4>依存元 (Source)</h4>
          <button
            type="button"
            className="dep-link-btn"
            onClick={() => onSelectNode(selectedEdge.source)}
          >
            {selectedEdge.source}
          </button>
        </div>

        <div className="details-section">
          <h4>依存先 (Target)</h4>
          <button
            type="button"
            className="dep-link-btn"
            onClick={() => onSelectNode(selectedEdge.target)}
          >
            {selectedEdge.target}
          </button>
        </div>

        <div className="limitations-box">
          <strong>エッジの変更判定</strong>
          <p className="muted-text text-xs">
            {selectedEdge.change_type === "added" &&
              "BASEコミットには存在せず、HEADコミットで新たに追加された依存関係です。"}
            {selectedEdge.change_type === "removed" &&
              "BASEコミットには存在していましたが、HEADコミットで削除された依存関係です。"}
            {selectedEdge.change_type === "unchanged" &&
              "BASEとHEADの双方に存在する不変の依存関係です。"}
          </p>
        </div>
      </aside>
    );
  }

  // If a node is selected
  if (selectedNode) {
    const isPackage = selectedNode.node_kind === "package";
    const baseM = selectedNode.base_metrics;
    const headM = selectedNode.head_metrics;

    return (
      <aside className="details-panel" aria-label="Diff詳細パネル" data-testid="diff-node-details">
        <div className="details-header">
          <div className="details-badges">
            {isPackage && <span className="badge badge-package">Go Package</span>}
            <span
              className={`badge ${
                selectedNode.change_type === "added"
                  ? "badge-diff-added"
                  : selectedNode.change_type === "removed"
                  ? "badge-diff-removed"
                  : selectedNode.change_type === "modified"
                  ? "badge-diff-modified"
                  : "badge-diff-unchanged"
              }`}
            >
              {selectedNode.change_type === "added"
                ? "+ 追加"
                : selectedNode.change_type === "removed"
                ? "- 削除"
                : selectedNode.change_type === "modified"
                ? "~ 変更あり"
                : "= 変更なし"}
            </span>
          </div>
          <h3 className="details-filepath">
            {isPackage ? `📦 ${selectedNode.path}` : selectedNode.path}
          </h3>
          {selectedNode.github_url && (
            <a
              href={selectedNode.github_url}
              target="_blank"
              rel="noopener noreferrer"
              className="btn btn-primary btn-full"
            >
              GitHubで開く ↗
            </a>
          )}
        </div>

        {selectedNode.change_details && selectedNode.change_details.length > 0 && (
          <div className="details-section" data-testid="diff-node-change-details">
            <h4>変更詳細 ({selectedNode.change_details.length}件)</h4>
            <ul className="reason-list">
              {selectedNode.change_details.map((detail, idx) => (
                <li key={idx} style={{ color: "#fbbf24" }}>
                  {detail}
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="details-section">
          <h4>BASE vs HEAD メトリクス比較</h4>
          <table style={{ width: "100%", fontSize: "0.85rem", borderCollapse: "collapse", marginTop: "6px" }}>
            <thead>
              <tr style={{ borderBottom: "1px solid var(--border-color)", textAlign: "left" }}>
                <th style={{ padding: "4px" }}>メトリクス</th>
                <th style={{ padding: "4px" }}>BASE</th>
                <th style={{ padding: "4px" }}>HEAD</th>
                <th style={{ padding: "4px" }}>差分</th>
              </tr>
            </thead>
            <tbody>
              <tr style={{ borderBottom: "1px solid rgba(255,255,255,0.05)" }}>
                <td style={{ padding: "4px" }}>関数定義数</td>
                <td style={{ padding: "4px" }}>{baseM ? baseM.functions : "-"}</td>
                <td style={{ padding: "4px" }}>{headM ? headM.functions : "-"}</td>
                <td style={{ padding: "4px", color: (headM?.functions ?? 0) - (baseM?.functions ?? 0) > 0 ? "#4ade80" : (headM?.functions ?? 0) - (baseM?.functions ?? 0) < 0 ? "#f87171" : "inherit" }}>
                  {baseM && headM ? (headM.functions - baseM.functions > 0 ? `+${headM.functions - baseM.functions}` : headM.functions - baseM.functions) : "-"}
                </td>
              </tr>
              <tr style={{ borderBottom: "1px solid rgba(255,255,255,0.05)" }}>
                <td style={{ padding: "4px" }}>{isPackage ? "型定義数" : "クラス定義数"}</td>
                <td style={{ padding: "4px" }}>{baseM ? (baseM.types_count ?? baseM.classes) : "-"}</td>
                <td style={{ padding: "4px" }}>{headM ? (headM.types_count ?? headM.classes) : "-"}</td>
                <td style={{ padding: "4px" }}>
                  {baseM && headM
                    ? `${(headM.types_count ?? headM.classes) - (baseM.types_count ?? baseM.classes) > 0 ? "+" : ""}${(headM.types_count ?? headM.classes) - (baseM.types_count ?? baseM.classes)}`
                    : "-"}
                </td>
              </tr>
              <tr style={{ borderBottom: "1px solid rgba(255,255,255,0.05)" }}>
                <td style={{ padding: "4px" }}>被依存数 (In-degree)</td>
                <td style={{ padding: "4px" }}>{baseM ? baseM.in_degree : "-"}</td>
                <td style={{ padding: "4px" }}>{headM ? headM.in_degree : "-"}</td>
                <td style={{ padding: "4px" }}>
                  {baseM && headM
                    ? `${headM.in_degree - baseM.in_degree > 0 ? "+" : ""}${headM.in_degree - baseM.in_degree}`
                    : "-"}
                </td>
              </tr>
              <tr style={{ borderBottom: "1px solid rgba(255,255,255,0.05)" }}>
                <td style={{ padding: "4px" }}>依存先数 (Out-degree)</td>
                <td style={{ padding: "4px" }}>{baseM ? baseM.out_degree : "-"}</td>
                <td style={{ padding: "4px" }}>{headM ? headM.out_degree : "-"}</td>
                <td style={{ padding: "4px" }}>
                  {baseM && headM
                    ? `${headM.out_degree - baseM.out_degree > 0 ? "+" : ""}${headM.out_degree - baseM.out_degree}`
                    : "-"}
                </td>
              </tr>
              <tr>
                <td style={{ padding: "4px" }}>重要度スコア</td>
                <td style={{ padding: "4px" }}>{baseM ? baseM.importance.toFixed(1) : "-"}</td>
                <td style={{ padding: "4px" }}>{headM ? headM.importance.toFixed(1) : "-"}</td>
                <td style={{ padding: "4px" }}>
                  {baseM && headM
                    ? `${(headM.importance - baseM.importance) > 0 ? "+" : ""}${(headM.importance - baseM.importance).toFixed(1)}`
                    : "-"}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </aside>
    );
  }

  // When nothing is selected, show general diff overview
  const addedCycles = cycleChanges.added_cycles || [];
  const resolvedCycles = cycleChanges.resolved_cycles || [];

  return (
    <aside className="details-panel" aria-label="Diff詳細パネル" data-testid="diff-overview-panel">
      <div className="details-empty">
        <h3>アーキテクチャ差分概要</h3>
        <p className="muted-text">
          ノードまたはエッジをクリックすると、変更詳細・メトリクス差分・循環依存の推移を表示します。
        </p>
      </div>

      <div className="stats-grid">
        <div className="stat-box">
          <span className="stat-label">追加ノード</span>
          <span className="stat-value" style={{ color: "#4ade80" }}>
            +{summary.added_nodes_count}
          </span>
        </div>
        <div className="stat-box">
          <span className="stat-label">削除ノード</span>
          <span className="stat-value" style={{ color: "#f87171" }}>
            -{summary.removed_nodes_count}
          </span>
        </div>
        <div className="stat-box">
          <span className="stat-label">変更ノード</span>
          <span className="stat-value" style={{ color: "#fbbf24" }}>
            ~{summary.modified_nodes_count}
          </span>
        </div>
        <div className="stat-box">
          <span className="stat-label">循環SCC (HEAD)</span>
          <span className="stat-value">
            {summary.head_cyclic_sccs}
          </span>
        </div>
      </div>

      {addedCycles.length > 0 && (
        <div className="details-section" data-testid="added-cycles-list">
          <h4 style={{ color: "#f87171" }}>⚠️ 新規発生した循環依存 ({addedCycles.length}件)</h4>
          <p className="muted-text text-xs">
            HEADコミットで新たに形成された強連結成分（SCC）です。
          </p>
          <ul className="dep-list">
            {addedCycles.map((cycle, i) => (
              <li key={i} className="dep-item">
                <div style={{ fontSize: "0.8rem", color: "#fca5a5" }}>
                  Cycle #{i + 1} ({cycle.length} nodes):
                </div>
                <div style={{ fontFamily: "monospace", fontSize: "0.75rem", marginTop: "2px" }}>
                  {cycle.join(" ➔ ")} ➔ {cycle[0]}
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      {resolvedCycles.length > 0 && (
        <div className="details-section" data-testid="resolved-cycles-list">
          <h4 style={{ color: "#4ade80" }}>✅ 解消された循環依存 ({resolvedCycles.length}件)</h4>
          <p className="muted-text text-xs">
            BASEコミットに存在していた循環依存が、HEADコミットで解消されました。
          </p>
          <ul className="dep-list">
            {resolvedCycles.map((cycle, i) => (
              <li key={i} className="dep-item">
                <div style={{ fontSize: "0.8rem", color: "#86efac" }}>
                  Resolved Cycle #{i + 1} ({cycle.length} nodes):
                </div>
                <div style={{ fontFamily: "monospace", fontSize: "0.75rem", marginTop: "2px" }}>
                  {cycle.join(" ➔ ")}
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="limitations-box">
        <strong>計算量・検出アルゴリズム</strong>
        <p className="muted-text text-xs">
          本Diff機能は Tarjan の強連結成分（SCC）アルゴリズムおよび決定論的グラフ比較（O(V + E)）を用いて、
          ノード・エッジの差分と循環依存の発生・解消を判定しています。
        </p>
      </div>
    </aside>
  );
}
