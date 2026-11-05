package diff

import (
	"sort"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/graph"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

// SCCResult holds strongly connected components detected in the dependency graph.
type SCCResult struct {
	CyclicSCCs       [][]string // each inner slice is a sorted list of node keys
	CyclicNodesCount int
	CyclicSCCCount   int
	NodeInCycle      map[string]bool
}

// DetectCycles runs Tarjan's strongly connected components algorithm in O(V + E) time.
// Single-node components without self-loops are not considered cyclic.
func DetectCycles(files []domain.SourceFile, edges []graph.Edge) SCCResult {
	if len(files) == 0 {
		return SCCResult{
			CyclicSCCs:  [][]string{},
			NodeInCycle: make(map[string]bool),
		}
	}

	// Build deterministic adjacency list
	adj := make(map[string][]string, len(files))
	selfLoops := make(map[string]bool)
	allNodes := make(map[string]bool, len(files))

	for _, f := range files {
		adj[f.Path] = []string{}
		allNodes[f.Path] = true
	}

	for _, e := range edges {
		if !allNodes[e.SourcePath] || !allNodes[e.TargetPath] {
			continue
		}
		if e.SourcePath == e.TargetPath {
			selfLoops[e.SourcePath] = true
			continue
		}
		adj[e.SourcePath] = append(adj[e.SourcePath], e.TargetPath)
	}

	for k := range adj {
		sort.Strings(adj[k])
	}

	// Tarjan state
	index := 0
	indices := make(map[string]int, len(files))
	lowlinks := make(map[string]int, len(files))
	onStack := make(map[string]bool, len(files))
	stack := make([]string, 0, len(files))
	var sccs [][]string

	var strongconnect func(v string)
	strongconnect = func(v string) {
		indices[v] = index
		lowlinks[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if _, visited := indices[w]; !visited {
				strongconnect(w)
				if lowlinks[w] < lowlinks[v] {
					lowlinks[v] = lowlinks[w]
				}
			} else if onStack[w] {
				if indices[w] < lowlinks[v] {
					lowlinks[v] = indices[w]
				}
			}
		}

		// Root of SCC
		if lowlinks[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}

			// Only consider cyclic if >= 2 nodes OR 1 node with self-loop
			if len(scc) > 1 || (len(scc) == 1 && selfLoops[scc[0]]) {
				sort.Strings(scc)
				sccs = append(sccs, scc)
			}
		}
	}

	// Deterministic traversal by sorted node keys
	sortedNodeKeys := make([]string, 0, len(files))
	for _, f := range files {
		sortedNodeKeys = append(sortedNodeKeys, f.Path)
	}
	sort.Strings(sortedNodeKeys)

	for _, v := range sortedNodeKeys {
		if _, visited := indices[v]; !visited {
			strongconnect(v)
		}
	}

	// Sort SCCs deterministically by first node key
	sort.Slice(sccs, func(i, j int) bool {
		return sccs[i][0] < sccs[j][0]
	})

	nodeInCycle := make(map[string]bool)
	totalCyclicNodes := 0
	for _, scc := range sccs {
		for _, n := range scc {
			if !nodeInCycle[n] {
				nodeInCycle[n] = true
				totalCyclicNodes++
			}
		}
	}

	return SCCResult{
		CyclicSCCs:       sccs,
		CyclicNodesCount: totalCyclicNodes,
		CyclicSCCCount:   len(sccs),
		NodeInCycle:      nodeInCycle,
	}
}
