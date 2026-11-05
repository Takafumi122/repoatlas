import React from "react";
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { PRImpactPanel } from "../components/PRImpactPanel";
import type { PRImpactResponse } from "../lib/types";

const mockImpact: PRImpactResponse = {
  pr_analysis_id: "pr-12345",
  head_analysis_id: "head-analysis-1",
  repository: {
    owner: "pallets",
    name: "itsdangerous",
    html_url: "https://github.com/pallets/itsdangerous",
  },
  pull_number: 352,
  title: "Update signer verification and URL safe serializer",
  base_sha: "abcdef1234567890abcdef1234567890abcdef12",
  head_sha: "1234567890abcdef1234567890abcdef12345678",
  summary: {
    changed_files_count: 2,
    direct_impact_count: 1,
    indirect_impact_count: 1,
    total_affected_count: 2,
    unanalyzed_files_count: 1,
    analysis_completeness: "complete",
  },
  changed_files: [
    {
      path: "src/itsdangerous/signer.py",
      status: "modified",
      additions: 12,
      deletions: 3,
      changes: 15,
      is_python: true,
      is_analyzed: true,
    },
    {
      path: "README.md",
      status: "modified",
      additions: 2,
      deletions: 1,
      changes: 3,
      is_python: false,
      is_analyzed: false,
      unanalyzed_reason: "non_python_file",
    },
  ],
  affected_files: [
    {
      node_id: "src/itsdangerous/url_safe.py",
      path: "src/itsdangerous/url_safe.py",
      depth: 1,
      impact_type: "direct",
      source_changed_files: ["src/itsdangerous/signer.py"],
      representative_dependency_path: [
        "src/itsdangerous/url_safe.py",
        "src/itsdangerous/signer.py",
      ],
      in_degree: 2,
      out_degree: 3,
      importance_score: 0.8,
      visual_role: "direct_impact",
    },
    {
      node_id: "src/itsdangerous/__init__.py",
      path: "src/itsdangerous/__init__.py",
      depth: 2,
      impact_type: "indirect",
      source_changed_files: ["src/itsdangerous/signer.py"],
      representative_dependency_path: [
        "src/itsdangerous/__init__.py",
        "src/itsdangerous/url_safe.py",
        "src/itsdangerous/signer.py",
      ],
      in_degree: 1,
      out_degree: 4,
      importance_score: 0.9,
      visual_role: "indirect_impact",
    },
  ],
  affected_edges: [
    {
      id: "edge-1",
      source: "src/itsdangerous/url_safe.py",
      target: "src/itsdangerous/signer.py",
      import_count: 1,
      evidence_count: 1,
    },
    {
      id: "edge-2",
      source: "src/itsdangerous/__init__.py",
      target: "src/itsdangerous/url_safe.py",
      import_count: 1,
      evidence_count: 1,
    },
  ],
  unanalyzed_files: [
    {
      path: "README.md",
      status: "modified",
      additions: 2,
      deletions: 1,
      changes: 3,
      is_python: false,
      is_analyzed: false,
      unanalyzed_reason: "non_python_file",
    },
  ],
};

describe("PRImpactPanel component", () => {
  it("renders PR title, number, commit SHAs, and summary KPIs accurately", () => {
    const html = renderToStaticMarkup(<PRImpactPanel impact={mockImpact} />);

    expect(html).toContain("PR #352");
    expect(html).toContain("Update signer verification and URL safe serializer");
    expect(html).toContain("解析完了 (Complete)");
    expect(html).toContain("abcdef1");
    expect(html).toContain("1234567");
    expect(html).toContain("変更ファイル (2)");
    expect(html).toContain("直接影響 (1)");
    expect(html).toContain("間接影響 (1)");
    expect(html).toContain("未解析 (1)");
    expect(html).toContain("src/itsdangerous/signer.py");
    expect(html).toContain("+12");
    expect(html).toContain("-3");
  });
});
