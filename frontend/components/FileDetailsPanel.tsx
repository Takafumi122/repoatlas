"use client";

import React from "react";
import type {
  ArchitectureEdgeData,
  ArchitectureNodeData,
  GraphEdgeData,
  GraphNodeData,
  GraphViewMode,
  GuidedTourStep,
  ImpactAnalysisResult,
} from "@/lib/types";

interface FileDetailsPanelProps {
  selectedNode: GraphNodeData | null;
  allNodes: GraphNodeData[];
  allEdges: GraphEdgeData[];
  recommendedIds: string[];
  guidedTour: GuidedTourStep[];
  impactResult: ImpactAnalysisResult | null;
  impactMaxDepth: number;
  onImpactMaxDepthChange: (depth: number) => void;
  impactHighlightActive: boolean;
  onImpactHighlightToggle: (active: boolean) => void;
  viewMode: GraphViewMode;
  selectedArchNode: ArchitectureNodeData | null;
  archEdges: ArchitectureEdgeData[];
  onSelectArchDirectory: (dirId: string) => void;
  onInspectFileFromArch: (nodeId: string) => void;
  warnings: string[];
  onSelectNode: (nodeId: string) => void;
}

export function FileDetailsPanel({
  selectedNode,
  allNodes,
  allEdges,
  recommendedIds,
  guidedTour,
  impactResult,
  impactMaxDepth,
  onImpactMaxDepthChange,
  impactHighlightActive,
  onImpactHighlightToggle,
  viewMode,
  selectedArchNode,
  archEdges,
  onSelectArchDirectory,
  onInspectFileFromArch,
  warnings,
  onSelectNode,
}: FileDetailsPanelProps) {
  const nodeById = new Map(allNodes.map((n) => [n.id, n]));
  const tourStep = selectedNode
    ? guidedTour.find((s) => s.node_id === selectedNode.id)
    : undefined;
  const isRecommended = Boolean(
    selectedNode && recommendedIds.includes(selectedNode.id)
  );

  // Architecture View: Directory Group Inspector
  if (viewMode === "architecture" && selectedArchNode) {
    const incomingDirs = archEdges.filter(
      (e) => e.target === selectedArchNode.id
    );
    const outgoingDirs = archEdges.filter(
      (e) => e.source === selectedArchNode.id
    );

    return (
      <aside
        className="details-panel"
        aria-label="ファイル詳細パネル"
        data-testid="arch-details-panel"
      >
        <div className="details-header">
          <div className="details-badges">
            <span className="badge badge-arch-dir">Architecture Group</span>
            {selectedArchNode.entrypoint_count > 0 && (
              <span className="badge badge-entrypoint">
                Entrypoint {selectedArchNode.entrypoint_count}件
              </span>
            )}
          </div>
          <h3 className="details-filepath">📁 {selectedArchNode.directory}</h3>
          <p className="muted-text">
            同一ディレクトリ内の {selectedArchNode.file_count}{" "}
            個のPythonファイルを集約表示しています。ファイル名をクリックするとFile
            Viewで詳細を確認できます。
          </p>
        </div>

        <div className="stats-grid">
          <div className="stat-box">
            <span className="stat-label">内部ファイル数</span>
            <span className="stat-value" data-testid="arch-file-count">
              {selectedArchNode.file_count}
            </span>
          </div>
          <div className="stat-box">
            <span className="stat-label">内部凝集エッジ</span>
            <span className="stat-value">
              {selectedArchNode.internal_edge_count}
            </span>
          </div>
          <div className="stat-box">
            <span className="stat-label">被依存ディレクトリ</span>
            <span className="stat-value">
              {selectedArchNode.in_degree} ({selectedArchNode.incoming_weight}件)
            </span>
          </div>
          <div className="stat-box">
            <span className="stat-label">依存先ディレクトリ</span>
            <span className="stat-value">
              {selectedArchNode.out_degree} ({selectedArchNode.outgoing_weight}件)
            </span>
          </div>
        </div>

        <div className="details-section">
          <h4>
            ディレクトリ内部ファイル一覧 ({selectedArchNode.files.length}件)
          </h4>
          <ul className="dep-list" data-testid="arch-internal-files-list">
            {selectedArchNode.files.map((f) => (
              <li key={f.node_id} className="dep-item">
                <button
                  type="button"
                  className="dep-link-btn"
                  onClick={() => onInspectFileFromArch(f.node_id)}
                  data-testid={`arch-file-item-${f.path}`}
                >
                  {f.path}
                </button>
                <div className="rec-reason">
                  Score {f.importance_score.toFixed(1)} / In: {f.in_degree} /
                  Out: {f.out_degree} / 関数: {f.functions} / クラス:{" "}
                  {f.classes}
                </div>
              </li>
            ))}
          </ul>
        </div>

        <div className="details-section">
          <h4>
            このディレクトリを利用する外部ディレクトリ ({incomingDirs.length}件)
          </h4>
          {incomingDirs.length === 0 ? (
            <p className="muted-text">
              他ディレクトリからのimportはありません
            </p>
          ) : (
            <ul className="dep-list">
              {incomingDirs.map((e) => (
                <li key={e.id} className="dep-item">
                  <button
                    type="button"
                    className="dep-link-btn"
                    onClick={() => onSelectArchDirectory(e.source)}
                  >
                    ← 📁 {e.source_directory} ({e.weight} imports)
                  </button>
                  {e.sample_pairs.length > 0 && (
                    <div className="rec-reason">例: {e.sample_pairs[0]}</div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="details-section">
          <h4>
            このディレクトリが依存する外部ディレクトリ ({outgoingDirs.length}件)
          </h4>
          {outgoingDirs.length === 0 ? (
            <p className="muted-text">
              他ディレクトリへのimportはありません
            </p>
          ) : (
            <ul className="dep-list">
              {outgoingDirs.map((e) => (
                <li key={e.id} className="dep-item">
                  <button
                    type="button"
                    className="dep-link-btn"
                    onClick={() => onSelectArchDirectory(e.target)}
                  >
                    → 📁 {e.target_directory} ({e.weight} imports)
                  </button>
                  {e.sample_pairs.length > 0 && (
                    <div className="rec-reason">例: {e.sample_pairs[0]}</div>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>
      </aside>
    );
  }

  if (!selectedNode) {
    return (
      <aside className="details-panel" aria-label="ファイル詳細パネル">
        <div className="details-empty">
          <h3>ファイル詳細・影響範囲</h3>
          <p className="muted-text">
            グラフ上のファイルノード、または上部の「Guided Code
            Tour」「推奨閲覧順」をクリックすると、関数・クラス数、BFSによる変更影響範囲（Impact
            Analysis）、GitHubソースへのリンクを表示します。
          </p>
        </div>

        <div className="details-section">
          <h4>Guided Code Tour（推奨閲覧順序: {guidedTour.length}件）</h4>
          <ul className="dep-list">
            {guidedTour.map((step) => (
              <li key={step.node_id} className="dep-item">
                <button
                  type="button"
                  className="dep-link-btn"
                  onClick={() => onSelectNode(step.node_id)}
                >
                  <span className="rank-num">Step {step.step}</span>
                  <span className="dep-path">{step.path}</span>
                </button>
                <div className="rec-reason">
                  [{step.role_label}] {step.reason}
                </div>
              </li>
            ))}
          </ul>
        </div>

        <div className="limitations-box">
          <strong>静的解析の注記</strong>
          <ul>
            {warnings.map((w, i) => (
              <li key={i}>{w}</li>
            ))}
            <li>
              Guided Code TourおよびImpact
              AnalysisはTree-sitterによる静的import解析から決定論的に算出された推定であり、動的import（importlib等）や実行時の影響を保証するものではありません。
            </li>
          </ul>
        </div>
      </aside>
    );
  }

  const incomingNodes = allEdges
    .filter((e) => e.target === selectedNode.id)
    .map((e) => nodeById.get(e.source))
    .filter((n): n is GraphNodeData => Boolean(n));

  const outgoingNodes = allEdges
    .filter((e) => e.source === selectedNode.id)
    .map((e) => nodeById.get(e.target))
    .filter((n): n is GraphNodeData => Boolean(n));

  const recommendationReasons: string[] = [];
  if (tourStep) {
    recommendationReasons.push(
      `Guided Tour Step ${tourStep.step} (${tourStep.role_label}): ${tourStep.reason}`
    );
  }
  if (selectedNode.is_entrypoint) {
    recommendationReasons.push("エントリーポイント候補ファイル名（+3.0 bonus）");
  }
  if (selectedNode.in_degree > 0) {
    recommendationReasons.push(
      `${selectedNode.in_degree}個の内部ファイルからimportされている中心モジュール`
    );
  }
  if (selectedNode.out_degree > 0) {
    recommendationReasons.push(
      `${selectedNode.out_degree}個の内部モジュールを束ねて利用`
    );
  }

  return (
    <aside className="details-panel" aria-label="ファイル詳細パネル">
      <div className="details-header">
        <div className="details-badges">
          {tourStep && (
            <span className="badge badge-tour">
              Tour Step #{tourStep.step} ({tourStep.role_label})
            </span>
          )}
          {selectedNode.is_entrypoint && (
            <span className="badge badge-entrypoint">Entrypoint候補</span>
          )}
          {isRecommended && (
            <span className="badge badge-recommended">優先閲覧推奨 Top 5</span>
          )}
        </div>
        <h3 className="details-filepath">{selectedNode.path}</h3>
        <a
          href={selectedNode.github_url}
          target="_blank"
          rel="noopener noreferrer"
          className="btn btn-primary btn-full"
        >
          GitHubでソースコードを開く ↗
        </a>
      </div>

      <div className="stats-grid">
        <div className="stat-box">
          <span className="stat-label">関数定義数</span>
          <span className="stat-value">{selectedNode.functions}</span>
        </div>
        <div className="stat-box">
          <span className="stat-label">クラス定義数</span>
          <span className="stat-value">{selectedNode.classes}</span>
        </div>
        <div className="stat-box">
          <span className="stat-label">被依存数 (In)</span>
          <span className="stat-value">{selectedNode.in_degree}</span>
        </div>
        <div className="stat-box">
          <span className="stat-label">依存先数 (Out)</span>
          <span className="stat-value">{selectedNode.out_degree}</span>
        </div>
      </div>

      <div className="details-section">
        <h4>
          重要度スコア: {selectedNode.importance_score.toFixed(1)} / 10.0
        </h4>
        {recommendationReasons.length > 0 ? (
          <ul className="reason-list">
            {recommendationReasons.map((r, i) => (
              <li key={i}>{r}</li>
            ))}
          </ul>
        ) : (
          <p className="muted-text">独立したモジュール</p>
        )}
      </div>

      {/* Phase 2 Task 2: Impact Analysis via BFS */}
      {impactResult && (
        <div
          className="details-section impact-section"
          data-testid="impact-analysis-section"
        >
          <div className="impact-header-row">
            <h4>Impact Analysis（潜在的影響範囲）</h4>
            <label className="impact-depth-label">
              探索深度:
              <select
                value={impactMaxDepth}
                onChange={(e) =>
                  onImpactMaxDepthChange(Number(e.target.value))
                }
                className="impact-depth-select"
                aria-label="影響探索深度"
                data-testid="impact-depth-select"
              >
                <option value={1}>Depth 1 (直接のみ)</option>
                <option value={2}>Depth 2</option>
                <option value={3}>Depth 3 (標準)</option>
                <option value={4}>Depth 4</option>
                <option value={5}>Depth 5</option>
                <option value={0}>無制限 (全到達)</option>
              </select>
            </label>
          </div>

          <div className="impact-summary-badges" data-testid="impact-summary">
            <span className="metric-pill">
              影響ファイル数:{" "}
              <strong data-testid="impact-total-count">
                {impactResult.total_affected}
              </strong>
              件
            </span>
            <span className="metric-pill">
              直接依存元:{" "}
              <strong data-testid="impact-direct-count">
                {impactResult.direct_count}
              </strong>
              件
            </span>
            <span className="metric-pill">
              間接依存元:{" "}
              <strong data-testid="impact-indirect-count">
                {impactResult.indirect_count}
              </strong>
              件
            </span>
          </div>

          <div className="impact-toggle-row">
            <button
              type="button"
              className={`chip-btn ${
                impactHighlightActive ? "chip-btn-active" : ""
              }`}
              onClick={() => onImpactHighlightToggle(!impactHighlightActive)}
              data-testid="impact-highlight-toggle"
            >
              {impactHighlightActive
                ? "✓ 影響範囲ハイライト中"
                : "影響範囲をハイライト"}
            </button>
          </div>

          {impactResult.affected_files.length === 0 ? (
            <p className="muted-text">
              このファイルに依存する内部ファイルはありません（影響ファイル 0件）。
            </p>
          ) : (
            <ul className="dep-list" data-testid="impact-files-list">
              {impactResult.affected_files.map((af) => (
                <li key={af.node_id} className="dep-item">
                  <div className="impact-item-top">
                    <button
                      type="button"
                      className="dep-link-btn"
                      onClick={() => onSelectNode(af.node_id)}
                    >
                      {af.path}
                    </button>
                    <span
                      className={`badge ${
                        af.direct
                          ? "badge-impact-direct"
                          : "badge-impact-indirect"
                      }`}
                    >
                      {af.direct
                        ? "直接依存元 (D1)"
                        : `間接依存元 (D${af.depth})`}
                    </span>
                  </div>
                  {!af.direct && (
                    <div className="rec-reason">
                      経路: {af.via_path} 経由で依存
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}

          <p className="impact-disclaimer" data-testid="impact-disclaimer">
            ※静的import依存からBFSで推定した潜在的影響範囲であり、実際の実行時の影響を保証するものではありません。
          </p>
        </div>
      )}

      <div className="details-section">
        <h4>被依存ファイル（このファイルをimport: {incomingNodes.length}件）</h4>
        {incomingNodes.length === 0 ? (
          <p className="muted-text">内部ファイルからのimportはありません</p>
        ) : (
          <ul className="dep-list">
            {incomingNodes.map((n) => (
              <li key={n.id} className="dep-item">
                <button
                  type="button"
                  className="dep-link-btn"
                  onClick={() => onSelectNode(n.id)}
                >
                  ← {n.path}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="details-section">
        <h4>依存先ファイル（このファイルがimport: {outgoingNodes.length}件）</h4>
        {outgoingNodes.length === 0 ? (
          <p className="muted-text">内部ファイルへのimportはありません</p>
        ) : (
          <ul className="dep-list">
            {outgoingNodes.map((n) => (
              <li key={n.id} className="dep-item">
                <button
                  type="button"
                  className="dep-link-btn"
                  onClick={() => onSelectNode(n.id)}
                >
                  → {n.path}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </aside>
  );
}
