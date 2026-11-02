package python

import (
	"path"
	"sort"
	"strings"
)

// ModuleIndex indexes repository Python files by module name and relative path
// supporting both the repository root ("") and a top-level "src/" directory.
type ModuleIndex struct {
	allFiles        map[string]bool
	byModule        map[string][]string
	internalTopDirs map[string]bool
	hasSrcRoot      bool
}

func BuildModuleIndex(filePaths []string) *ModuleIndex {
	idx := &ModuleIndex{
		allFiles:        make(map[string]bool, len(filePaths)),
		byModule:        make(map[string][]string),
		internalTopDirs: make(map[string]bool),
	}

	sortedPaths := make([]string, len(filePaths))
	copy(sortedPaths, filePaths)
	sort.Strings(sortedPaths)

	for _, rawPath := range sortedPaths {
		p := normalizeRelPath(rawPath)
		if p == "" || !strings.HasSuffix(p, ".py") {
			continue
		}
		idx.allFiles[p] = true

		// Index relative to repository root
		if mod := filePathToModuleName(p); mod != "" {
			idx.addModuleCandidate(mod, p)
			top := topSegment(mod)
			if top != "" && top != "src" {
				idx.internalTopDirs[top] = true
			}
		}

		// Also index relative to top-level src/ if applicable
		if strings.HasPrefix(p, "src/") {
			idx.hasSrcRoot = true
			sub := strings.TrimPrefix(p, "src/")
			if mod := filePathToModuleName(sub); mod != "" {
				idx.addModuleCandidate(mod, p)
				top := topSegment(mod)
				if top != "" {
					idx.internalTopDirs[top] = true
				}
			}
		}
	}

	return idx
}

func (idx *ModuleIndex) addModuleCandidate(moduleName, filePath string) {
	existing := idx.byModule[moduleName]
	for _, p := range existing {
		if p == filePath {
			return
		}
	}
	idx.byModule[moduleName] = append(existing, filePath)
}

// LookupModule returns (resolvedFilePath, ambiguous, found).
func (idx *ModuleIndex) LookupModule(moduleName string) (string, bool, bool) {
	candidates := idx.byModule[moduleName]
	if len(candidates) == 0 {
		return "", false, false
	}
	if len(candidates) == 1 {
		return candidates[0], false, true
	}
	return "", true, true
}

// HasFile checks if an exact relative file path exists in the repository.
func (idx *ModuleIndex) HasFile(relPath string) bool {
	return idx.allFiles[normalizeRelPath(relPath)]
}

// IsInternalTopPackage returns true if topName matches a known top-level module or package in the repo.
func (idx *ModuleIndex) IsInternalTopPackage(topName string) bool {
	if topName == "" {
		return false
	}
	return idx.internalTopDirs[topName]
}

func filePathToModuleName(relPath string) string {
	trimmed := strings.TrimSuffix(relPath, ".py")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return ""
	}
	if parts[len(parts)-1] == "__init__" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return ""
	}
	for _, seg := range parts {
		if seg == "" {
			return ""
		}
	}
	return strings.Join(parts, ".")
}

func topSegment(dotted string) string {
	if i := strings.IndexByte(dotted, '.'); i >= 0 {
		return dotted[:i]
	}
	return dotted
}

func normalizeRelPath(p string) string {
	cleaned := path.Clean(strings.TrimPrefix(p, "/"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}
