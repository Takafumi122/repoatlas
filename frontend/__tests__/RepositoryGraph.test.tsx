import React from "react";
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { FALLBACK_DEMO_GRAPH } from "@/lib/api";
import {
  buildArchitectureGraph,
  buildGuidedTour,
  buildLayoutedElements,
  computeImpactAnalysis,
  extractTopDirectory,
  filterAndCapGraph,
} from "@/lib/graph-layout";
import { RepositoryGraph } from "@/components/RepositoryGraph";

describe("graph-layout utilities", () => {
  it("extracts top directory or (root) accurately", () => {
    expect(extractTopDirectory("main.py")).toBe("(root)");
    expect(extractTopDirectory("src/pkg/core.py")).toBe("src");
  });

  it("filters nodes by search query and preserves matching edges", () => {
    const res = filterAndCapGraph(
      FALLBACK_DEMO_GRAPH.nodes,
      FALLBACK_DEMO_GRAPH.edges,
      FALLBACK_DEMO_GRAPH.recommended_file_ids,
      "auth",
      "ALL"
    );
    expect(res.visibleNodes).toHaveLength(1);
    expect(res.visibleNodes[0].path).toBe("app/auth.py");
    expect(res.visibleEdges).toHaveLength(0);
  });

  it("caps visible nodes at maxNodes limit and reports hiddenByLimitCount", () => {
    const res = filterAndCapGraph(
      FALLBACK_DEMO_GRAPH.nodes,
      FALLBACK_DEMO_GRAPH.edges,
      FALLBACK_DEMO_GRAPH.recommended_file_ids,
      "",
      "ALL",
      3
    );
    expect(res.visibleNodes).toHaveLength(3);
    expect(res.hiddenByLimitCount).toBe(FALLBACK_DEMO_GRAPH.nodes.length - 3);
  });

  it("computes dagre layout positions and highlights related nodes/edges", () => {
    const { flowNodes, flowEdges } = buildLayoutedElements(
      FALLBACK_DEMO_GRAPH.nodes,
      FALLBACK_DEMO_GRAPH.edges,
      FALLBACK_DEMO_GRAPH.recommended_file_ids,
      "file:app/main.py"
    );

    expect(flowNodes).toHaveLength(FALLBACK_DEMO_GRAPH.nodes.length);
    expect(flowEdges).toHaveLength(FALLBACK_DEMO_GRAPH.edges.length);

    const mainNode = flowNodes.find((n) => n.id === "file:app/main.py");
    expect(mainNode?.data.isSelected).toBe(true);
    expect(Number.isFinite(mainNode?.position.x)).toBe(true);
    expect(Number.isFinite(mainNode?.position.y)).toBe(true);
  });

  it("generates Guided Code Tour starting with entrypoint and including reasons", () => {
    const tour = buildGuidedTour(
      FALLBACK_DEMO_GRAPH.nodes,
      FALLBACK_DEMO_GRAPH.edges
    );
    expect(tour.length).toBeGreaterThanOrEqual(5);
    expect(tour.length).toBeLessThanOrEqual(10);
    expect(tour[0].path).toBe("app/main.py");
    expect(tour[0].role).toBe("entrypoint");
    expect(tour[0].reason.length).toBeGreaterThan(0);
  });

  it("computes BFS Impact Analysis with depth limits and cycle safety", () => {
    // In FALLBACK_DEMO_GRAPH:
    // app/db.py is directly imported by app/main.py (D1) and app/models.py (D1)
    // app/models.py is imported by app/auth.py (D2) and app/routes.py (D2)
    // app/routes.py is imported by app/main.py (already D1) and app/__init__.py (D3)
    const d1 = computeImpactAnalysis(
      FALLBACK_DEMO_GRAPH.nodes,
      FALLBACK_DEMO_GRAPH.edges,
      "file:app/db.py",
      1
    );
    expect(d1?.total_affected).toBe(2);
    expect(d1?.direct_count).toBe(2);
    expect(d1?.indirect_count).toBe(0);

    const d2 = computeImpactAnalysis(
      FALLBACK_DEMO_GRAPH.nodes,
      FALLBACK_DEMO_GRAPH.edges,
      "file:app/db.py",
      2
    );
    expect(d2?.total_affected).toBe(4);
    expect(d2?.direct_count).toBe(2);
    expect(d2?.indirect_count).toBe(2);
  });

  it("aggregates files and inter-directory edges for Architecture View", () => {
    const arch = buildArchitectureGraph(
      FALLBACK_DEMO_GRAPH.nodes,
      FALLBACK_DEMO_GRAPH.edges
    );
    expect(arch.nodes).toHaveLength(1);
    expect(arch.nodes[0].directory).toBe("app");
    expect(arch.nodes[0].file_count).toBe(7);
    expect(arch.nodes[0].internal_edge_count).toBe(11);
  });
});

describe("RepositoryGraph component", () => {
  it("renders demo badge, Guided Code Tour, Impact Analysis, View Switcher, and details panel", () => {
    const html = renderToStaticMarkup(
      <RepositoryGraph graph={FALLBACK_DEMO_GRAPH} />
    );

    expect(html).toContain("DEMO DATASET");
    expect(html).toContain("repoatlas-demo/python-web-starter");
    expect(html).toContain("#1 app/config.py");
    expect(html).toContain("Guided Code Tour");
    expect(html).toContain("Impact Analysis（潜在的影響範囲）");
    expect(html).toContain("Architecture View");
    expect(html).toContain("GitHubでソースコードを開く");
  });
});
