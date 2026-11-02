import dagre from "@dagrejs/dagre";
import { describe, expect, it } from "vitest";
import type { GraphEdgeData, GraphNodeData } from "@/lib/types";
import {
  applyGraphVisualState,
  buildArchitectureGraph,
  buildArchitectureLayoutElements,
  buildGuidedTour,
  computeDagreNodePositions,
  computeImpactAnalysis,
  filterAndCapGraph,
} from "@/lib/graph-layout";

function generateSyntheticGraph(fileCount: number): {
  nodes: GraphNodeData[];
  edges: GraphEdgeData[];
} {
  const dirCount = Math.max(5, Math.floor(fileCount / 25));
  const nodes: GraphNodeData[] = [];
  const edges: GraphEdgeData[] = [];

  for (let i = 0; i < fileCount; i++) {
    const dirIdx = i % dirCount;
    const isEntry = i === 0 || i === 1;
    const fileName = isEntry ? (i === 0 ? "main.py" : "cli.py") : `mod_${i}.py`;
    const path = `src/pkg_${String(dirIdx).padStart(2, "0")}/${fileName}`;
    const inDeg = (i * 3) % 9;
    const outDeg = (i * 2) % 6;
    const score = Math.min(10, 2 * inDeg + 0.5 * outDeg + (isEntry ? 3 : 0));

    nodes.push({
      id: `file:${path}`,
      path,
      functions: (i % 7) + 1,
      classes: i % 4,
      in_degree: inDeg,
      out_degree: outDeg,
      is_entrypoint: isEntry,
      importance_score: score,
      github_url: `https://github.com/bench/repo/blob/deadbeef/${path}`,
    });
  }

  for (let i = 0; i < fileCount; i++) {
    const target1 = (i + 1) % fileCount;
    const target2 = (i + 7) % fileCount;
    edges.push({
      source: nodes[i].id,
      target: nodes[target1].id,
      kind: "import",
    });
    if (i % 2 === 0 && target2 !== i) {
      edges.push({
        source: nodes[i].id,
        target: nodes[target2].id,
        kind: "import",
      });
    }
  }

  return { nodes, edges };
}

function measureUnoptimizedDagreSelection(
  nodes: GraphNodeData[],
  edges: GraphEdgeData[]
): number {
  const t0 = performance.now();
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: "LR", nodesep: 42, ranksep: 90, ranker: "network-simplex" });
  for (const n of nodes) {
    g.setNode(n.id, { width: 240, height: 86 });
  }
  for (const e of edges) {
    g.setEdge(e.source, e.target);
  }
  dagre.layout(g);
  return performance.now() - t0;
}

describe("Graph rendering & layout performance benchmarks (50, 300, 1000 files)", () => {
  it("measures and validates performance across 50, 300, and 1000 file repositories", () => {
    const sizes = [50, 300, 1000];
    const report: Record<string, unknown>[] = [];

    for (const size of sizes) {
      const { nodes, edges } = generateSyntheticGraph(size);

      // 1. Tour + Architecture + Impact data generation
      const tGen0 = performance.now();
      const tour = buildGuidedTour(nodes, edges);
      const arch = buildArchitectureGraph(nodes, edges);
      const impact = computeImpactAnalysis(nodes, edges, nodes[10].id, 3);
      const dataGenMs = performance.now() - tGen0;

      // 2. Filter & 300-node cap
      const tFilter0 = performance.now();
      const filtered = filterAndCapGraph(
        nodes,
        edges,
        tour.slice(0, 5).map((s) => s.node_id),
        "",
        "ALL"
      );
      const filterMs = performance.now() - tFilter0;

      // 3. Before optimization: full network-simplex Dagre layout on raw graph vs capped graph
      const unoptimizedRawLayoutMs = measureUnoptimizedDagreSelection(
        size <= 300 ? nodes : filtered.visibleNodes,
        size <= 300 ? edges : filtered.visibleEdges
      );

      // 4. After optimization: Initial Dagre layout (adaptive ranker + 300 cap)
      const tLayout0 = performance.now();
      const positions = computeDagreNodePositions(
        filtered.visibleNodes,
        filtered.visibleEdges
      );
      const optimizedInitialLayoutMs = performance.now() - tLayout0;

      // 5. After optimization: Interactive node selection / impact highlight update (no Dagre re-run)
      const tSelect0 = performance.now();
      const visual = applyGraphVisualState(
        filtered.visibleNodes,
        filtered.visibleEdges,
        positions,
        tour.slice(0, 5).map((s) => s.node_id),
        nodes[10].id,
        impact
      );
      const interactiveSelectionMs = performance.now() - tSelect0;

      // 6. Architecture View layout (directory-aggregated)
      const tArch0 = performance.now();
      const archLayout = buildArchitectureLayoutElements(arch, arch.nodes[0]?.id || null);
      const archLayoutMs = performance.now() - tArch0;

      expect(visual.flowNodes.length).toBeLessThanOrEqual(300);
      expect(archLayout.flowNodes.length).toBe(arch.nodes.length);
      expect(interactiveSelectionMs).toBeLessThan(15);

      report.push({
        files: size,
        edges: edges.length,
        renderedNodes: filtered.visibleNodes.length,
        archDirs: arch.nodes.length,
        dataGenMs: Number(dataGenMs.toFixed(2)),
        filterMs: Number(filterMs.toFixed(2)),
        beforeSelectOrLayoutMs: Number(unoptimizedRawLayoutMs.toFixed(2)),
        afterInitialLayoutMs: Number(optimizedInitialLayoutMs.toFixed(2)),
        afterInteractiveSelectMs: Number(interactiveSelectionMs.toFixed(2)),
        archViewLayoutMs: Number(archLayoutMs.toFixed(2)),
      });
    }

    // Output benchmark table for documentation
    console.table(report);
  });
});
