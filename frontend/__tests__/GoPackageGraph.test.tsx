import React from "react";
import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import type { GraphNodeData, GraphResponse, ImportReference } from "@/lib/types";
import { RepositoryGraph } from "@/components/RepositoryGraph";
import { FileDetailsPanel } from "@/components/FileDetailsPanel";

const mockGoPackageNode: GraphNodeData = {
  id: "pkg:github.com/spf13/cobra",
  node_kind: "package",
  language: "go",
  path: "cobra",
  module_path: "github.com/spf13/cobra",
  package_import_path: "github.com/spf13/cobra",
  directory_path: ".",
  package_name: "cobra",
  files_count: 3,
  functions: 12,
  methods_count: 24,
  types_count: 5,
  classes: 5,
  in_degree: 4,
  out_degree: 2,
  is_entrypoint: false,
  importance_score: 8.5,
  github_url: "https://github.com/spf13/cobra/tree/main",
  files: [
    { path: "command.go", functions: 5, methods: 10, types: 2 },
    { path: "args.go", functions: 4, methods: 8, types: 1 },
    { path: "command_win.go", functions: 3, methods: 6, types: 2, build_tags: "windows" },
  ],
};

const mockGoGraphResponse: GraphResponse = {
  analysis_id: "test-go-analysis-1",
  repository: {
    owner: "spf13",
    name: "cobra",
    html_url: "https://github.com/spf13/cobra",
  },
  commit_sha: "cd378035b3769971bc18bfb672778c187be06ad9",
  language: "go",
  graph_granularity: "package",
  summary: {
    files: 3,
    internal_edges: 2,
    external_imports: 4,
    unresolved_imports: 0,
  },
  nodes: [mockGoPackageNode],
  edges: [],
  recommended_file_ids: ["pkg:github.com/spf13/cobra"],
  warnings: [],
};

const mockEvidence: ImportReference[] = [
  {
    source_path: "cobra",
    source_file: "command.go",
    source_package: "github.com/spf13/cobra",
    target_path: "",
    target_package: "github.com/spf13/pflag",
    import_text: `import "github.com/spf13/pflag"`,
    module_name: "github.com/spf13/pflag",
    line_number: 18,
    column_number: 1,
    classification: "external",
    build_constraints: "none",
  },
  {
    source_path: "cobra",
    source_file: "command_win.go",
    source_package: "github.com/spf13/cobra",
    target_path: "",
    target_package: "golang.org/x/sys/windows",
    import_text: `import "golang.org/x/sys/windows"`,
    module_name: "golang.org/x/sys/windows",
    line_number: 12,
    column_number: 1,
    classification: "external",
    build_constraints: "windows",
  },
];

describe("Go Package Graph Components", () => {
  it("renders RepositoryGraph with Go Package nodes and metrics", () => {
    const html = renderToStaticMarkup(
      <RepositoryGraph graph={mockGoGraphResponse} />
    );

    expect(html).toContain("Go Pkg");
    expect(html).toContain("📦 cobra");
    expect(html).toContain("Files: 3");
    expect(html).toContain("In: 4");
    expect(html).toContain("Out: 2");
  });

  it("renders FileDetailsPanel for Go Package with package members and import evidence", () => {
    const html = renderToStaticMarkup(
      <FileDetailsPanel
        selectedNode={mockGoPackageNode}
        allNodes={[mockGoPackageNode]}
        allEdges={[]}
        recommendedIds={["pkg:github.com/spf13/cobra"]}
        guidedTour={[]}
        impactResult={null}
        impactMaxDepth={3}
        onImpactMaxDepthChange={() => {}}
        impactHighlightActive={false}
        onImpactHighlightToggle={() => {}}
        viewMode="file"
        selectedArchNode={null}
        archEdges={[]}
        onSelectArchDirectory={() => {}}
        onInspectFileFromArch={() => {}}
        warnings={[]}
        onSelectNode={() => {}}
        importEvidence={mockEvidence}
      />
    );

    // Package metadata
    expect(html).toContain("Go Package");
    expect(html).toContain("github.com/spf13/cobra");
    expect(html).toContain("所属Goファイル一覧 (3件)");
    expect(html).toContain("command.go");
    expect(html).toContain("command_win.go");
    expect(html).toContain("tags: windows");

    // Import Evidence
    expect(html).toContain("Import根拠・証跡 (2件)");
    expect(html).toContain("command.go:18");
    expect(html).toContain('import &quot;github.com/spf13/pflag&quot;');
    expect(html).toContain("Build tags: windows");
  });
});
