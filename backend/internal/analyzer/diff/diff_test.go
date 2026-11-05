package diff_test

import (
	"fmt"
	"testing"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/diff"
	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

func TestComputeGraphDiff_BasicAddRemoveModify(t *testing.T) {
	baseFiles := []domain.SourceFile{
		{Path: "app/main.py", FunctionsCount: 2, ClassesCount: 0, InDegree: 0, OutDegree: 2},
		{Path: "app/util.py", FunctionsCount: 3, ClassesCount: 1, InDegree: 1, OutDegree: 0},
		{Path: "app/legacy.py", FunctionsCount: 1, ClassesCount: 0, InDegree: 1, OutDegree: 0},
	}
	baseEdges := []graph.Edge{
		{SourcePath: "app/main.py", TargetPath: "app/util.py", Kind: "import"},
		{SourcePath: "app/main.py", TargetPath: "app/legacy.py", Kind: "import"},
	}

	headFiles := []domain.SourceFile{
		{Path: "app/main.py", FunctionsCount: 3, ClassesCount: 0, InDegree: 0, OutDegree: 2}, // modified (func 2->3)
		{Path: "app/util.py", FunctionsCount: 3, ClassesCount: 1, InDegree: 2, OutDegree: 0}, // modified (indegree 1->2)
		{Path: "app/service.py", FunctionsCount: 4, ClassesCount: 2, InDegree: 0, OutDegree: 1}, // added
		// legacy.py is removed
	}
	headEdges := []graph.Edge{
		{SourcePath: "app/main.py", TargetPath: "app/util.py", Kind: "import"}, // unchanged
		{SourcePath: "app/service.py", TargetPath: "app/util.py", Kind: "import"}, // added
		// main -> legacy is removed
	}

	res := diff.ComputeGraphDiff(
		"1111111111111111111111111111111111111111",
		"2222222222222222222222222222222222222222",
		"python", "file",
		baseFiles, baseEdges,
		headFiles, headEdges,
	)

	if res.Summary.AddedNodesCount != 1 {
		t.Errorf("expected 1 added node, got %d", res.Summary.AddedNodesCount)
	}
	if res.Summary.RemovedNodesCount != 1 {
		t.Errorf("expected 1 removed node, got %d", res.Summary.RemovedNodesCount)
	}
	if res.Summary.ModifiedNodesCount != 2 {
		t.Errorf("expected 2 modified nodes, got %d", res.Summary.ModifiedNodesCount)
	}
	if res.Summary.AddedEdgesCount != 1 {
		t.Errorf("expected 1 added edge, got %d", res.Summary.AddedEdgesCount)
	}
	if res.Summary.RemovedEdgesCount != 1 {
		t.Errorf("expected 1 removed edge, got %d", res.Summary.RemovedEdgesCount)
	}

	nodeMap := make(map[string]diff.DiffNode)
	for _, n := range res.Nodes {
		nodeMap[n.Path] = n
	}

	if nodeMap["app/service.py"].ChangeType != domain.NodeChangeAdded {
		t.Errorf("expected app/service.py to be added")
	}
	if nodeMap["app/legacy.py"].ChangeType != domain.NodeChangeRemoved {
		t.Errorf("expected app/legacy.py to be removed")
	}
	if nodeMap["app/main.py"].ChangeType != domain.NodeChangeModified {
		t.Errorf("expected app/main.py to be modified")
	}
	if nodeMap["app/main.py"].FunctionsDiff != 1 {
		t.Errorf("expected functions diff +1 for main.py, got %d", nodeMap["app/main.py"].FunctionsDiff)
	}
}

func TestComputeGraphDiff_CyclesDetectionAndDiff(t *testing.T) {
	// Base: A -> B -> C (no cycle)
	baseFiles := []domain.SourceFile{
		{Path: "a.go", InDegree: 0, OutDegree: 1, Language: "go"},
		{Path: "b.go", InDegree: 1, OutDegree: 1, Language: "go"},
		{Path: "c.go", InDegree: 1, OutDegree: 0, Language: "go"},
	}
	baseEdges := []graph.Edge{
		{SourcePath: "a.go", TargetPath: "b.go"},
		{SourcePath: "b.go", TargetPath: "c.go"},
	}

	// Head: A -> B -> C -> A (cycle introduced: A, B, C)
	headFiles := []domain.SourceFile{
		{Path: "a.go", InDegree: 1, OutDegree: 1, Language: "go"},
		{Path: "b.go", InDegree: 1, OutDegree: 1, Language: "go"},
		{Path: "c.go", InDegree: 1, OutDegree: 1, Language: "go"},
	}
	headEdges := []graph.Edge{
		{SourcePath: "a.go", TargetPath: "b.go"},
		{SourcePath: "b.go", TargetPath: "c.go"},
		{SourcePath: "c.go", TargetPath: "a.go"},
	}

	res := diff.ComputeGraphDiff(
		"base-sha", "head-sha", "go", "package",
		baseFiles, baseEdges,
		headFiles, headEdges,
	)

	if res.Summary.BaseCyclicSCCs != 0 {
		t.Errorf("expected base cyclic SCCs 0, got %d", res.Summary.BaseCyclicSCCs)
	}
	if res.Summary.HeadCyclicSCCs != 1 {
		t.Errorf("expected head cyclic SCCs 1, got %d", res.Summary.HeadCyclicSCCs)
	}
	if len(res.CycleChanges.AddedCycles) != 1 {
		t.Fatalf("expected 1 added cycle, got %d", len(res.CycleChanges.AddedCycles))
	}
	if len(res.CycleChanges.ResolvedCycles) != 0 {
		t.Errorf("expected 0 resolved cycles, got %d", len(res.CycleChanges.ResolvedCycles))
	}

	// Reverse case: Cycle resolved
	resResolved := diff.ComputeGraphDiff(
		"head-sha", "base-sha", "go", "package",
		headFiles, headEdges,
		baseFiles, baseEdges,
	)
	if len(resResolved.CycleChanges.ResolvedCycles) != 1 {
		t.Errorf("expected 1 resolved cycle when reverting, got %d", len(resResolved.CycleChanges.ResolvedCycles))
	}
}

func TestComputeGraphDiff_IdenticalGraphs(t *testing.T) {
	files := []domain.SourceFile{
		{Path: "main.py", FunctionsCount: 2, ClassesCount: 1, InDegree: 0, OutDegree: 1},
		{Path: "util.py", FunctionsCount: 5, ClassesCount: 0, InDegree: 1, OutDegree: 0},
	}
	edges := []graph.Edge{
		{SourcePath: "main.py", TargetPath: "util.py", Kind: "import"},
	}

	res := diff.ComputeGraphDiff("sha1", "sha2", "python", "file", files, edges, files, edges)
	if res.Summary.AddedNodesCount != 0 || res.Summary.RemovedNodesCount != 0 || res.Summary.ModifiedNodesCount != 0 {
		t.Errorf("expected 0 node changes on identical graphs")
	}
	if res.Summary.AddedEdgesCount != 0 || res.Summary.RemovedEdgesCount != 0 {
		t.Errorf("expected 0 edge changes on identical graphs")
	}
	for _, n := range res.Nodes {
		if n.ChangeType != domain.NodeChangeUnchanged {
			t.Errorf("node %s expected unchanged, got %s", n.Path, n.ChangeType)
		}
	}
	for _, e := range res.Edges {
		if e.ChangeType != domain.EdgeChangeUnchanged {
			t.Errorf("edge %s->%s expected unchanged, got %s", e.SourcePath, e.TargetPath, e.ChangeType)
		}
	}
}

func TestComputeGraphDiff_ScaleAndDeterminism(t *testing.T) {
	const n = 300
	var baseFiles []domain.SourceFile
	var headFiles []domain.SourceFile
	var baseEdges []graph.Edge
	var headEdges []graph.Edge

	for i := 0; i < n; i++ {
		path := fmt.Sprintf("pkg/mod%03d.go", i)
		bf := domain.SourceFile{Path: path, FunctionsCount: i, Language: "go", InDegree: 1, OutDegree: 1}
		hf := domain.SourceFile{Path: path, FunctionsCount: i + (i % 2), Language: "go", InDegree: 1, OutDegree: 1}
		baseFiles = append(baseFiles, bf)
		headFiles = append(headFiles, hf)
		if i > 0 {
			prev := fmt.Sprintf("pkg/mod%03d.go", i-1)
			baseEdges = append(baseEdges, graph.Edge{SourcePath: prev, TargetPath: path})
			headEdges = append(headEdges, graph.Edge{SourcePath: prev, TargetPath: path})
		}
	}

	res1 := diff.ComputeGraphDiff("b", "h", "go", "package", baseFiles, baseEdges, headFiles, headEdges)
	res2 := diff.ComputeGraphDiff("b", "h", "go", "package", baseFiles, baseEdges, headFiles, headEdges)

	if len(res1.Nodes) != len(res2.Nodes) || len(res1.Edges) != len(res2.Edges) {
		t.Fatalf("determinism failure: length mismatch")
	}

	for i := range res1.Nodes {
		if res1.Nodes[i].ID != res2.Nodes[i].ID || res1.Nodes[i].ChangeType != res2.Nodes[i].ChangeType {
			t.Fatalf("determinism failure at node %d", i)
		}
	}
}
