"use client";

import React, { useMemo, useState } from "react";
import dagre from "@dagrejs/dagre";
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import type {
  ArchitectureDiffGraphResponse,
  DiffNodeData,
  DiffEdgeData,
} from "@/lib/types";
import { DiffNode, type DiffNodeViewModel } from "./DiffNode";
import {
  DiffSummaryBanner,
  type DiffFilterType,
  type DiffViewMode,
} from "./DiffSummaryBanner";
import { DiffDetailsPanel } from "./DiffDetailsPanel";

const nodeTypes = {
  diffNode: DiffNode,
};

const NODE_WIDTH = 240;
const NODE_HEIGHT = 86;

interface ArchitectureDiffCanvasProps {
  diffData: ArchitectureDiffGraphResponse;
}

export function ArchitectureDiffCanvas({ diffData }: ArchitectureDiffCanvasProps) {
  const [viewMode, setViewMode] = useState<DiffViewMode>("diff");
  const [filter, setFilter] = useState<DiffFilterType>("all");
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);

  // 1. Compute stable Dagre positions using the UNION of all nodes and edges
  const layoutPositions = useMemo(() => {
    const g = new dagre.graphlib.Graph();
    g.setDefaultEdgeLabel(() => ({}));
    g.setGraph({
      rankdir: "LR",
      nodesep: 40,
      ranksep: 80,
      marginx: 30,
      marginy: 30,
    });

    for (const node of diffData.nodes) {
      g.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
    }

    for (const edge of diffData.edges) {
      g.setEdge(edge.source, edge.target);
    }

    dagre.layout(g);

    const positions = new Map<string, { x: number; y: number }>();
    for (const node of diffData.nodes) {
      const pos = g.node(node.id);
      if (pos) {
        positions.set(node.id, {
          x: pos.x - NODE_WIDTH / 2,
          y: pos.y - NODE_HEIGHT / 2,
        });
      } else {
        positions.set(node.id, { x: 0, y: 0 });
      }
    }
    return positions;
  }, [diffData.nodes, diffData.edges]);

  // Set of cyclic node IDs from cycle_changes
  const cyclicNodeIDs = useMemo(() => {
    const set = new Set<string>();
    const allCycles = [
      ...(diffData.cycle_changes.added_cycles || []),
      ...(diffData.cycle_changes.resolved_cycles || []),
      ...(diffData.cycle_changes.persistent_cycles || []),
    ];
    for (const cycle of allCycles) {
      for (const id of cycle) {
        set.add(id);
      }
    }
    return set;
  }, [diffData.cycle_changes]);

  // 2. Filter nodes based on viewMode and filter
  const { visibleNodes, visibleEdges } = useMemo(() => {
    let nodes = diffData.nodes;
    let edges = diffData.edges;

    // View Mode filtering
    if (viewMode === "base") {
      // BASE only: nodes and edges present in BASE
      nodes = nodes.filter((n) => n.change_type !== "added");
      const baseNodeSet = new Set(nodes.map((n) => n.id));
      edges = edges.filter(
        (e) =>
          e.change_type !== "added" &&
          baseNodeSet.has(e.source) &&
          baseNodeSet.has(e.target)
      );
    } else if (viewMode === "head") {
      // HEAD only: nodes and edges present in HEAD
      nodes = nodes.filter((n) => n.change_type !== "removed");
      const headNodeSet = new Set(nodes.map((n) => n.id));
      edges = edges.filter(
        (e) =>
          e.change_type !== "removed" &&
          headNodeSet.has(e.source) &&
          headNodeSet.has(e.target)
      );
    }

    // Filter pill filtering
    const visibleNodeIDs = new Set<string>();
    for (const n of nodes) {
      let keep = true;
      if (filter === "added") {
        keep = n.change_type === "added";
      } else if (filter === "removed") {
        keep = n.change_type === "removed";
      } else if (filter === "modified") {
        keep = n.change_type === "modified";
      } else if (filter === "cyclic") {
        keep = cyclicNodeIDs.has(n.id) || cyclicNodeIDs.has(n.path);
      }
      if (keep) {
        visibleNodeIDs.add(n.id);
      }
    }

    const filteredNodes = nodes.map((n) => ({
      ...n,
      isSelected: n.id === selectedNodeId,
      isDimmed: filter !== "all" && !visibleNodeIDs.has(n.id),
    }));

    const finalEdges = edges.filter(
      (e) =>
        nodes.some((n) => n.id === e.source) &&
        nodes.some((n) => n.id === e.target)
    );

    return { visibleNodes: filteredNodes, visibleEdges: finalEdges };
  }, [
    diffData.nodes,
    diffData.edges,
    viewMode,
    filter,
    selectedNodeId,
    cyclicNodeIDs,
  ]);

  // 3. React Flow Node elements
  const rfNodes: Node<DiffNodeViewModel>[] = useMemo(() => {
    return visibleNodes.map((n) => {
      const pos = layoutPositions.get(n.id) || { x: 0, y: 0 };
      return {
        id: n.id,
        type: "diffNode",
        position: pos,
        data: { diffNode: n },
      };
    });
  }, [visibleNodes, layoutPositions]);

  // 4. React Flow Edge elements
  const rfEdges: Edge[] = useMemo(() => {
    return visibleEdges.map((e) => {
      let strokeColor = "#64748b"; // unchanged
      let strokeDasharray: string | undefined = undefined;
      let strokeWidth = 1.5;
      let animated = false;

      if (e.is_cyclic) {
        strokeColor = "#dc2626";
        strokeWidth = 3;
        animated = true;
      } else if (e.change_type === "added") {
        strokeColor = "#22c55e";
        strokeWidth = 2;
      } else if (e.change_type === "removed") {
        strokeColor = "#ef4444";
        strokeWidth = 2;
        strokeDasharray = "5, 5";
      }

      if (e.id === selectedEdgeId) {
        strokeColor = "#38bdf8";
        strokeWidth = 3;
      }

      return {
        id: e.id,
        source: e.source,
        target: e.target,
        type: "smoothstep",
        animated,
        style: {
          stroke: strokeColor,
          strokeWidth,
          strokeDasharray,
        },
      };
    });
  }, [visibleEdges, selectedEdgeId]);

  // Selected node & edge lookup
  const selectedNode = useMemo(
    () => diffData.nodes.find((n) => n.id === selectedNodeId) || null,
    [diffData.nodes, selectedNodeId]
  );

  const selectedEdge = useMemo(
    () => diffData.edges.find((e) => e.id === selectedEdgeId) || null,
    [diffData.edges, selectedEdgeId]
  );

  return (
    <div style={{ display: "flex", flexDirection: "column", height: "100vh", background: "var(--bg-main)" }}>
      <DiffSummaryBanner
        summary={diffData.summary}
        cycleChanges={diffData.cycle_changes}
        viewMode={viewMode}
        onViewModeChange={setViewMode}
        filter={filter}
        onFilterChange={setFilter}
      />

      <div style={{ display: "flex", flex: 1, position: "relative", overflow: "hidden" }}>
        <div style={{ flex: 1, position: "relative" }} data-testid="diff-flow-container">
          <ReactFlow
            nodes={rfNodes}
            edges={rfEdges}
            nodeTypes={nodeTypes}
            fitView
            minZoom={0.15}
            maxZoom={2.0}
            onNodeClick={(_, node) => {
              setSelectedNodeId(node.id);
              setSelectedEdgeId(null);
            }}
            onEdgeClick={(_, edge) => {
              setSelectedEdgeId(edge.id);
              setSelectedNodeId(null);
            }}
            onPaneClick={() => {
              setSelectedNodeId(null);
              setSelectedEdgeId(null);
            }}
          >
            <Background color="#334155" gap={24} size={1} />
            <Controls />
            <MiniMap
              nodeColor={(node) => {
                const dn = (node.data as DiffNodeViewModel)?.diffNode;
                if (!dn) return "#64748b";
                if (dn.change_type === "added") return "#22c55e";
                if (dn.change_type === "removed") return "#ef4444";
                if (dn.change_type === "modified") return "#f59e0b";
                return "#475569";
              }}
            />
          </ReactFlow>

          {/* Graph Legend */}
          <div className="graph-legend" data-testid="diff-legend">
            <span className="legend-item">
              <span className="legend-color-dot" style={{ background: "#22c55e" }} />
              <span>追加 (Added)</span>
            </span>
            <span className="legend-item">
              <span className="legend-color-dot" style={{ background: "#ef4444" }} />
              <span>削除 (Removed)</span>
            </span>
            <span className="legend-item">
              <span className="legend-color-dot" style={{ background: "#f59e0b" }} />
              <span>変更 (Modified)</span>
            </span>
            <span className="legend-item">
              <span className="legend-color-dot" style={{ background: "#dc2626" }} />
              <span>循環依存 (Cyclic SCC)</span>
            </span>
          </div>
        </div>

        <div className="graph-details-pane">
          <DiffDetailsPanel
            selectedNode={selectedNode}
            selectedEdge={selectedEdge}
            summary={diffData.summary}
            cycleChanges={diffData.cycle_changes}
            onSelectNode={(id) => {
              setSelectedNodeId(id);
              setSelectedEdgeId(null);
            }}
          />
        </div>
      </div>
    </div>
  );
}
