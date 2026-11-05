package graph

import (
	"fmt"
	"path"
	"sort"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

// ArchitectureFileSummary represents a file inside a directory group node.
type ArchitectureFileSummary struct {
	NodeID          string  `json:"node_id"`
	Path            string  `json:"path"`
	Functions       int     `json:"functions"`
	Classes         int     `json:"classes"`
	InDegree        int     `json:"in_degree"`
	OutDegree       int     `json:"out_degree"`
	IsEntrypoint    bool    `json:"is_entrypoint"`
	ImportanceScore float64 `json:"importance_score"`
}

// ArchitectureNode represents a directory-level aggregated node in the Architecture View.
type ArchitectureNode struct {
	ID                string                    `json:"id"`
	Directory         string                    `json:"directory"`
	FileCount         int                       `json:"file_count"`
	FunctionsCount    int                       `json:"functions_count"`
	ClassesCount      int                       `json:"classes_count"`
	InternalEdgeCount int                       `json:"internal_edge_count"`
	InDegree          int                       `json:"in_degree"`
	OutDegree         int                       `json:"out_degree"`
	IncomingWeight    int                       `json:"incoming_weight"`
	OutgoingWeight    int                       `json:"outgoing_weight"`
	EntrypointCount   int                       `json:"entrypoint_count"`
	Files             []ArchitectureFileSummary `json:"files"`
}

// ArchitectureEdge represents an aggregated dependency edge between two directories.
type ArchitectureEdge struct {
	ID              string   `json:"id"`
	Source          string   `json:"source"`
	Target          string   `json:"target"`
	SourceDirectory string   `json:"source_directory"`
	TargetDirectory string   `json:"target_directory"`
	Weight          int      `json:"weight"`
	SamplePairs     []string `json:"sample_pairs"`
}

// ArchitectureGraph holds the directory-aggregated nodes and inter-directory edges.
type ArchitectureGraph struct {
	Nodes []ArchitectureNode `json:"nodes"`
	Edges []ArchitectureEdge `json:"edges"`
}

// DirectoryForPath returns the normalized directory path for a file path, or "(root)" for top-level files.
func DirectoryForPath(filePath string) string {
	dir := path.Dir(filePath)
	if dir == "." || dir == "" || dir == "/" {
		return "(root)"
	}
	return dir
}

// DirectoryNodeID returns the canonical node ID for a directory.
func DirectoryNodeID(dir string) string {
	return "dir:" + dir
}

// BuildArchitectureGraph aggregates file-level nodes and edges into a directory-level dependency graph.
func BuildArchitectureGraph(files []domain.SourceFile, edges []Edge) ArchitectureGraph {
	if len(files) == 0 {
		return ArchitectureGraph{
			Nodes: []ArchitectureNode{},
			Edges: []ArchitectureEdge{},
		}
	}

	fileByPath := make(map[string]domain.SourceFile, len(files))
	dirNodesMap := make(map[string]*ArchitectureNode)

	getDirectory := func(f domain.SourceFile) string {
		if f.Language == "go" || f.NodeKind == "package" {
			if f.DirectoryPath != "" && f.DirectoryPath != "." {
				return f.DirectoryPath
			}
			return "(root)"
		}
		return DirectoryForPath(f.Path)
	}

	for _, f := range files {
		fileByPath[f.Path] = f
		dir := getDirectory(f)
		node, ok := dirNodesMap[dir]
		if !ok {
			node = &ArchitectureNode{
				ID:        DirectoryNodeID(dir),
				Directory: dir,
				Files:     []ArchitectureFileSummary{},
			}
			dirNodesMap[dir] = node
		}
		node.FileCount++
		node.FunctionsCount += f.FunctionsCount
		node.ClassesCount += f.ClassesCount
		if f.IsEntrypoint {
			node.EntrypointCount++
		}
		nodeID := domain.NodeIDForPath(f.Path)
		if f.Language == "go" || f.NodeKind == "package" {
			if f.PackageImportPath != "" {
				nodeID = domain.NodeIDForPackage(f.PackageImportPath)
			} else {
				nodeID = domain.NodeIDForPackage(f.Path)
			}
		}
		node.Files = append(node.Files, ArchitectureFileSummary{
			NodeID:          nodeID,
			Path:            f.Path,
			Functions:       f.FunctionsCount,
			Classes:         f.ClassesCount,
			InDegree:        f.InDegree,
			OutDegree:       f.OutDegree,
			IsEntrypoint:    f.IsEntrypoint,
			ImportanceScore: f.ImportanceScore,
		})
	}

	type dirPair struct {
		src string
		dst string
	}
	edgeBuckets := make(map[dirPair][]string)
	seenFileEdge := make(map[[2]string]bool, len(edges))

	for _, e := range edges {
		if e.SourcePath == "" || e.TargetPath == "" || e.SourcePath == e.TargetPath {
			continue
		}
		srcFile, ok := fileByPath[e.SourcePath]
		if !ok {
			continue
		}
		dstFile, ok := fileByPath[e.TargetPath]
		if !ok {
			continue
		}
		fileKey := [2]string{e.SourcePath, e.TargetPath}
		if seenFileEdge[fileKey] {
			continue
		}
		seenFileEdge[fileKey] = true

		srcDir := getDirectory(srcFile)
		dstDir := getDirectory(dstFile)
		if srcDir == dstDir {
			if n, exists := dirNodesMap[srcDir]; exists {
				n.InternalEdgeCount++
			}
			continue
		}

		pair := dirPair{src: srcDir, dst: dstDir}
		edgeBuckets[pair] = append(edgeBuckets[pair], fmt.Sprintf("%s -> %s", e.SourcePath, e.TargetPath))
	}

	var archEdges []ArchitectureEdge
	for pair, samples := range edgeBuckets {
		sort.Strings(samples)
		cappedSamples := samples
		if len(cappedSamples) > 5 {
			cappedSamples = cappedSamples[:5]
		}
		weight := len(samples)

		srcNode, srcOk := dirNodesMap[pair.src]
		dstNode, dstOk := dirNodesMap[pair.dst]
		if srcOk && dstOk {
			srcNode.OutDegree++
			srcNode.OutgoingWeight += weight
			dstNode.InDegree++
			dstNode.IncomingWeight += weight
		}

		archEdges = append(archEdges, ArchitectureEdge{
			ID:              fmt.Sprintf("%s->%s", DirectoryNodeID(pair.src), DirectoryNodeID(pair.dst)),
			Source:          DirectoryNodeID(pair.src),
			Target:          DirectoryNodeID(pair.dst),
			SourceDirectory: pair.src,
			TargetDirectory: pair.dst,
			Weight:          weight,
			SamplePairs:     cappedSamples,
		})
	}

	sort.Slice(archEdges, func(i, j int) bool {
		if archEdges[i].SourceDirectory != archEdges[j].SourceDirectory {
			return archEdges[i].SourceDirectory < archEdges[j].SourceDirectory
		}
		return archEdges[i].TargetDirectory < archEdges[j].TargetDirectory
	})

	archNodes := make([]ArchitectureNode, 0, len(dirNodesMap))
	for _, n := range dirNodesMap {
		sort.Slice(n.Files, func(i, j int) bool {
			if n.Files[i].ImportanceScore != n.Files[j].ImportanceScore {
				return n.Files[i].ImportanceScore > n.Files[j].ImportanceScore
			}
			return n.Files[i].Path < n.Files[j].Path
		})
		archNodes = append(archNodes, *n)
	}

	sort.Slice(archNodes, func(i, j int) bool {
		return archNodes[i].Directory < archNodes[j].Directory
	})

	if archEdges == nil {
		archEdges = []ArchitectureEdge{}
	}

	return ArchitectureGraph{
		Nodes: archNodes,
		Edges: archEdges,
	}
}
