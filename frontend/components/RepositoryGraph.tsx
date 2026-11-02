"use client";

import React, { useMemo, useState } from "react";
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  type NodeMouseHandler,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import type { GraphResponse, GraphViewMode } from "@/lib/types";
import {
  applyGraphVisualState,
  buildArchitectureGraph,
  buildArchitectureLayoutElements,
  buildGuidedTour,
  computeDagreNodePositions,
  computeImpactAnalysis,
  filterAndCapGraph,
} from "@/lib/graph-layout";
import { FileNode } from "./FileNode";
import { DirectoryNode } from "./DirectoryNode";
import { FileDetailsPanel } from "./FileDetailsPanel";
import { GraphToolbar } from "./GraphToolbar";
import { GuidedTourBar } from "./GuidedTourBar";

const nodeTypes = {
  fileNode: FileNode,
  directoryNode: DirectoryNode,
};

interface RepositoryGraphProps {
  graph: GraphResponse;
}

export function RepositoryGraph({ graph }: RepositoryGraphProps) {
  const guidedTour = useMemo(
    () => buildGuidedTour(graph.nodes, graph.edges, graph.guided_tour),
    [graph.nodes, graph.edges, graph.guided_tour]
  );

  const architectureGraph = useMemo(
    () => buildArchitectureGraph(graph.nodes, graph.edges, graph.architecture),
    [graph.nodes, graph.edges, graph.architecture]
  );

  const tourStepById = useMemo(
    () => new Map(guidedTour.map((s) => [s.node_id, s.step])),
    [guidedTour]
  );

  const [viewMode, setViewMode] = useState<GraphViewMode>("file");
  const [searchQuery, setSearchQuery] = useState("");
  const [selectedDirectory, setSelectedDirectory] = useState("ALL");
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(
    guidedTour[0]?.node_id ||
      graph.recommended_file_ids[0] ||
      graph.nodes[0]?.id ||
      null
  );
  const [selectedArchDirId, setSelectedArchDirId] = useState<string | null>(
    architectureGraph.nodes[0]?.id || null
  );
  const [impactMaxDepth, setImpactMaxDepth] = useState<number>(3);
  const [impactHighlightActive, setImpactHighlightActive] =
    useState<boolean>(true);
  const [mobileTab, setMobileTab] = useState<"graph" | "details">("graph");

  const filtered = useMemo(
    () =>
      filterAndCapGraph(
        graph.nodes,
        graph.edges,
        graph.recommended_file_ids,
        searchQuery,
        selectedDirectory
      ),
    [
      graph.nodes,
      graph.edges,
      graph.recommended_file_ids,
      searchQuery,
      selectedDirectory,
    ]
  );

  const impactResult = useMemo(() => {
    if (!selectedNodeId) return null;
    return computeImpactAnalysis(
      graph.nodes,
      graph.edges,
      selectedNodeId,
      impactMaxDepth
    );
  }, [graph.nodes, graph.edges, selectedNodeId, impactMaxDepth]);

  // Step 1: Compute Dagre positions ONLY when visibleNodes or visibleEdges change
  const dagrePositions = useMemo(
    () =>
      computeDagreNodePositions(filtered.visibleNodes, filtered.visibleEdges),
    [filtered.visibleNodes, filtered.visibleEdges]
  );

  // Step 2: Apply selection & impact highlighting in <0.2ms without re-running Dagre
  const fileLayout = useMemo(
    () =>
      applyGraphVisualState(
        filtered.visibleNodes,
        filtered.visibleEdges,
        dagrePositions,
        graph.recommended_file_ids,
        selectedNodeId,
        impactHighlightActive ? impactResult : null,
        tourStepById
      ),
    [
      filtered.visibleNodes,
      filtered.visibleEdges,
      dagrePositions,
      graph.recommended_file_ids,
      selectedNodeId,
      impactHighlightActive,
      impactResult,
      tourStepById,
    ]
  );

  const archLayout = useMemo(
    () =>
      buildArchitectureLayoutElements(architectureGraph, selectedArchDirId),
    [architectureGraph, selectedArchDirId]
  );

  const selectedNode = useMemo(
    () => graph.nodes.find((n) => n.id === selectedNodeId) || null,
    [graph.nodes, selectedNodeId]
  );

  const selectedArchNode = useMemo(
    () =>
      architectureGraph.nodes.find((n) => n.id === selectedArchDirId) ||
      architectureGraph.nodes[0] ||
      null,
    [architectureGraph.nodes, selectedArchDirId]
  );

  const handleNodeClick: NodeMouseHandler<Node> = (_event, node) => {
    if (viewMode === "architecture") {
      setSelectedArchDirId(node.id);
      setMobileTab("details");
      return;
    }
    setSelectedNodeId(node.id);
    setMobileTab("details");
  };

  const activeNodes =
    viewMode === "architecture" ? archLayout.flowNodes : fileLayout.flowNodes;
  const activeEdges =
    viewMode === "architecture" ? archLayout.flowEdges : fileLayout.flowEdges;

  return (
    <div className="graph-workspace">
      <GraphToolbar
        graph={graph}
        visibleNodeCount={
          viewMode === "architecture"
            ? architectureGraph.nodes.length
            : filtered.visibleNodes.length
        }
        hiddenByLimitCount={filtered.hiddenByLimitCount}
        directories={filtered.directories}
        selectedDirectory={selectedDirectory}
        onDirectoryChange={(dir) => {
          setSelectedDirectory(dir);
          if (dir !== "ALL" && viewMode === "architecture") {
            setViewMode("file");
          }
        }}
        searchQuery={searchQuery}
        onSearchChange={(q) => {
          setSearchQuery(q);
          if (q && viewMode === "architecture") {
            setViewMode("file");
          }
        }}
        selectedNodeId={selectedNodeId}
        onSelectNode={(id) => {
          setSelectedNodeId(id);
          if (id) setMobileTab("details");
        }}
        viewMode={viewMode}
        onViewModeChange={(mode) => {
          setViewMode(mode);
          if (
            mode === "architecture" &&
            !selectedArchDirId &&
            architectureGraph.nodes[0]
          ) {
            setSelectedArchDirId(architectureGraph.nodes[0].id);
          }
        }}
        architectureNodeCount={architectureGraph.nodes.length}
      />

      <GuidedTourBar
        steps={guidedTour}
        selectedNodeId={selectedNodeId}
        onSelectStep={(nodeId) => {
          if (viewMode !== "file") {
            setViewMode("file");
          }
          setSelectedNodeId(nodeId);
          setMobileTab("details");
        }}
      />

      <div className="mobile-view-tabs">
        <button
          type="button"
          className={`tab-btn ${mobileTab === "graph" ? "tab-btn-active" : ""}`}
          onClick={() => setMobileTab("graph")}
        >
          {viewMode === "architecture"
            ? `Architecture View (${architectureGraph.nodes.length})`
            : `依存グラフ (${filtered.visibleNodes.length})`}
        </button>
        <button
          type="button"
          className={`tab-btn ${mobileTab === "details" ? "tab-btn-active" : ""}`}
          onClick={() => setMobileTab("details")}
        >
          {viewMode === "architecture"
            ? `ディレクトリ詳細 ${
                selectedArchNode ? `(${selectedArchNode.directory})` : ""
              }`
            : `ファイル詳細 ${selectedNode ? `(${selectedNode.path})` : ""}`}
        </button>
      </div>

      <div className="graph-main-split">
        <div
          className={`graph-canvas-pane ${
            mobileTab === "details" ? "hide-on-mobile" : ""
          }`}
        >
          {activeNodes.length === 0 ? (
            <div className="empty-graph-state">
              <h3>表示対象のPythonファイルがありません</h3>
              <p className="muted-text">
                {graph.nodes.length === 0
                  ? "このリポジトリには解析対象の .py ファイルが見つかりませんでした。"
                  : "検索条件またはディレクトリフィルタに一致するファイルがありません。"}
              </p>
            </div>
          ) : (
            <ReactFlow
              key={viewMode}
              nodes={activeNodes}
              edges={activeEdges}
              nodeTypes={nodeTypes}
              onNodeClick={handleNodeClick}
              onPaneClick={() => {
                if (viewMode === "file") {
                  setSelectedNodeId(null);
                }
              }}
              fitView
              minZoom={0.12}
              maxZoom={1.8}
              proOptions={{ hideAttribution: true }}
            >
              <Background color="#1e293b" gap={22} />
              <Controls showInteractive={false} />
              <MiniMap
                pannable
                zoomable
                nodeColor={(n) => {
                  const activeId =
                    viewMode === "architecture"
                      ? selectedArchDirId
                      : selectedNodeId;
                  if (n.id === activeId) return "#38bdf8";
                  return "#475569";
                }}
              />
            </ReactFlow>
          )}

          {/* Graph Legend Overlay */}
          <div
            className="graph-legend"
            aria-label="グラフ凡例"
            data-testid="graph-legend"
          >
            <div className="legend-title">凡例 (Legend)</div>
            <div className="legend-items">
              <span className="legend-item">
                <span className="legend-dot legend-dot-entry" />
                Entrypoint
              </span>
              <span className="legend-item">
                <span className="legend-dot legend-dot-tour" />
                Guided Tour / Top
              </span>
              <span className="legend-item">
                <span className="legend-dot legend-dot-selected" />
                選択中
              </span>
              <span className="legend-item">
                <span className="legend-dot legend-dot-impact1" />
                直接影響 (D1)
              </span>
              <span className="legend-item">
                <span className="legend-dot legend-dot-impact2" />
                間接影響 (D2+)
              </span>
              <span className="legend-item legend-edge-hint">
                <code>A ──▶ B</code> = A imports B
              </span>
            </div>
          </div>
        </div>

        <div
          className={`graph-details-pane ${
            mobileTab === "graph" ? "hide-on-mobile" : ""
          }`}
        >
          <FileDetailsPanel
            selectedNode={selectedNode}
            allNodes={graph.nodes}
            allEdges={graph.edges}
            recommendedIds={graph.recommended_file_ids}
            guidedTour={guidedTour}
            impactResult={impactResult}
            impactMaxDepth={impactMaxDepth}
            onImpactMaxDepthChange={setImpactMaxDepth}
            impactHighlightActive={impactHighlightActive}
            onImpactHighlightToggle={setImpactHighlightActive}
            viewMode={viewMode}
            selectedArchNode={selectedArchNode}
            archEdges={architectureGraph.edges}
            onSelectArchDirectory={(dirId) => setSelectedArchDirId(dirId)}
            onInspectFileFromArch={(nodeId) => {
              setViewMode("file");
              setSelectedNodeId(nodeId);
            }}
            warnings={graph.warnings}
            onSelectNode={(id) => {
              if (viewMode !== "file") {
                setViewMode("file");
              }
              setSelectedNodeId(id);
            }}
          />
        </div>
      </div>
    </div>
  );
}
