import React from "react";
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { ArchitectureDiffGraphResponse } from "@/lib/types";
import { DiffSummaryBanner } from "@/components/DiffSummaryBanner";
import { DiffDetailsPanel } from "@/components/DiffDetailsPanel";
import { ArchitectureDiffCanvas } from "@/components/ArchitectureDiffCanvas";

const mockDiffResponse: ArchitectureDiffGraphResponse = {
  diff_id: "diff-test-123",
  repository: {
    owner: "spf13",
    name: "cobra",
    html_url: "https://github.com/spf13/cobra",
  },
  base_sha: "1111111111111111111111111111111111111111",
  head_sha: "2222222222222222222222222222222222222222",
  language: "go",
  granularity: "package",
  status: "completed",
  summary: {
    base_commit_sha: "1111111111111111111111111111111111111111",
    head_commit_sha: "2222222222222222222222222222222222222222",
    language: "go",
    granularity: "package",
    total_base_nodes: 3,
    total_head_nodes: 3,
    total_base_edges: 2,
    total_head_edges: 3,
    added_nodes_count: 1,
    removed_nodes_count: 1,
    modified_nodes_count: 1,
    added_edges_count: 2,
    removed_edges_count: 1,
    base_cyclic_sccs: 0,
    head_cyclic_sccs: 1,
    metrics: {
      total_nodes: { base: 3, head: 3, diff: 0 },
      total_edges: { base: 2, head: 3, diff: 1 },
      added_nodes: 1,
      removed_nodes: 1,
      modified_nodes: 1,
      added_edges: 2,
      removed_edges: 1,
      cyclic_scc_count: { base: 0, head: 1, diff: 1 },
      cyclic_nodes_count: { base: 0, head: 2, diff: 2 },
      avg_in_degree: { base: 0.67, head: 1.0, diff: 0.33 },
      avg_out_degree: { base: 0.67, head: 1.0, diff: 0.33 },
      max_in_degree: { base: 1, head: 2, diff: 1 },
      max_out_degree: { base: 1, head: 2, diff: 1 },
    },
  },
  nodes: [
    {
      id: "pkg:github.com/spf13/cobra/cmd",
      node_kind: "package",
      language: "go",
      path: "cmd",
      change_type: "modified",
      base_metrics: {
        functions: 5,
        classes: 0,
        in_degree: 0,
        out_degree: 1,
        importance: 4.0,
      },
      head_metrics: {
        functions: 7,
        classes: 0,
        in_degree: 1,
        out_degree: 2,
        importance: 6.0,
      },
      change_details: [
        "Functions changed: 5 -> 7",
        "Out-degree changed: 1 -> 2",
      ],
      github_url: "https://github.com/spf13/cobra/tree/2222222222222222222222222222222222222222/cmd",
    },
    {
      id: "pkg:github.com/spf13/cobra/pkg/old",
      node_kind: "package",
      language: "go",
      path: "pkg/old",
      change_type: "removed",
      base_metrics: {
        functions: 2,
        classes: 1,
        in_degree: 1,
        out_degree: 0,
        importance: 2.0,
      },
      change_details: ["Package removed in HEAD"],
      github_url: "https://github.com/spf13/cobra/tree/1111111111111111111111111111111111111111/pkg/old",
    },
    {
      id: "pkg:github.com/spf13/cobra/pkg/new",
      node_kind: "package",
      language: "go",
      path: "pkg/new",
      change_type: "added",
      head_metrics: {
        functions: 3,
        classes: 2,
        in_degree: 1,
        out_degree: 1,
        importance: 3.5,
      },
      change_details: ["Package added in HEAD"],
      github_url: "https://github.com/spf13/cobra/tree/2222222222222222222222222222222222222222/pkg/new",
    },
  ],
  edges: [
    {
      id: "pkg:github.com/spf13/cobra/cmd->pkg:github.com/spf13/cobra/pkg/new",
      source: "pkg:github.com/spf13/cobra/cmd",
      target: "pkg:github.com/spf13/cobra/pkg/new",
      change_type: "added",
      is_cyclic: true,
      cycle_change_type: "added",
    },
    {
      id: "pkg:github.com/spf13/cobra/pkg/new->pkg:github.com/spf13/cobra/cmd",
      source: "pkg:github.com/spf13/cobra/pkg/new",
      target: "pkg:github.com/spf13/cobra/cmd",
      change_type: "added",
      is_cyclic: true,
      cycle_change_type: "added",
    },
  ],
  cycle_changes: {
    added_cycles: [
      [
        "pkg:github.com/spf13/cobra/cmd",
        "pkg:github.com/spf13/cobra/pkg/new",
      ],
    ],
    resolved_cycles: [],
    persistent_cycles: [],
  },
  warnings: [],
};

describe("Architecture Diff Components", () => {
  it("renders DiffSummaryBanner with commit hashes, counts, and cycle warnings", () => {
    const html = renderToStaticMarkup(
      <DiffSummaryBanner
        summary={mockDiffResponse.summary}
        cycleChanges={mockDiffResponse.cycle_changes}
        viewMode="diff"
        onViewModeChange={() => {}}
        filter="all"
        onFilterChange={() => {}}
      />
    );

    expect(html).toContain("1111111");
    expect(html).toContain("2222222");
    expect(html).toContain("+1 ノード");
    expect(html).toContain("-1 ノード");
    expect(html).toContain("~1 変更");
    expect(html).toContain("+1 循環依存発生");
    expect(html).toContain("Diff View");
    expect(html).toContain("BASE");
    expect(html).toContain("HEAD");
  });

  it("renders DiffDetailsPanel with BASE vs HEAD metrics comparison for modified node", () => {
    const modifiedNode = mockDiffResponse.nodes[0];
    const html = renderToStaticMarkup(
      <DiffDetailsPanel
        selectedNode={modifiedNode}
        selectedEdge={null}
        summary={mockDiffResponse.summary}
        cycleChanges={mockDiffResponse.cycle_changes}
        onSelectNode={() => {}}
      />
    );

    expect(html).toContain("cmd");
    expect(html).toContain("~ 変更あり");
    expect(html).toContain("Functions changed: 5 -&gt; 7");
    expect(html).toContain("BASE vs HEAD メトリクス比較");
    expect(html).toContain("+2"); // 7 - 5 = +2
  });

  it("renders DiffDetailsPanel with cyclic edge information", () => {
    const cyclicEdge = mockDiffResponse.edges[0];
    const html = renderToStaticMarkup(
      <DiffDetailsPanel
        selectedNode={null}
        selectedEdge={cyclicEdge}
        summary={mockDiffResponse.summary}
        cycleChanges={mockDiffResponse.cycle_changes}
        onSelectNode={() => {}}
      />
    );

    expect(html).toContain("+ 追加された依存");
    expect(html).toContain("循環依存発生 (Added Cycle)");
    expect(html).toContain("pkg:github.com/spf13/cobra/cmd");
    expect(html).toContain("pkg:github.com/spf13/cobra/pkg/new");
  });

  it("renders ArchitectureDiffCanvas containing ReactFlow, minimap, and legend", () => {
    const html = renderToStaticMarkup(
      <ArchitectureDiffCanvas diffData={mockDiffResponse} />
    );

    expect(html).toContain("diff-flow-container");
    expect(html).toContain("追加 (Added)");
    expect(html).toContain("削除 (Removed)");
    expect(html).toContain("変更 (Modified)");
    expect(html).toContain("循環依存 (Cyclic SCC)");
  });
});
