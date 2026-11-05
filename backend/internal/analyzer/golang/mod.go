package golang

import (
	"fmt"
	"path"
	"strings"

	"golang.org/x/mod/modfile"
)

// ModuleInfo describes a parsed Go module inside the repository.
type ModuleInfo struct {
	ModulePath string // e.g. "github.com/example/myapp"
	Dir        string // e.g. "." or "sub/mod"
	ModFile    string // e.g. "go.mod" or "sub/mod/go.mod"
	Replaces   map[string]string // old import path -> local directory relative to repo root
}

// ModuleIndex indexes all modules and workspaces found within the repository.
type ModuleIndex struct {
	ModulesByDir       map[string]*ModuleInfo // repo-relative dir -> module
	ModulesByPath      map[string][]*ModuleInfo // module path -> list of modules (for detecting ambiguities)
	LocalReplacements  map[string]string      // old import path -> target local dir
	AmbiguousModules   map[string]bool        // module paths with multiple differing directories
}

// NewModuleIndex initializes an empty module index.
func NewModuleIndex() *ModuleIndex {
	return &ModuleIndex{
		ModulesByDir:      make(map[string]*ModuleInfo),
		ModulesByPath:     make(map[string][]*ModuleInfo),
		LocalReplacements: make(map[string]string),
		AmbiguousModules:  make(map[string]bool),
	}
}

// CleanRepoDir normalizes directory paths within a repository (root is ".").
func CleanRepoDir(d string) string {
	d = path.Clean(strings.TrimPrefix(d, "/"))
	if d == "" || d == "." || d == "/" {
		return "."
	}
	return d
}

// ParseGoMod parses a go.mod file content and registers the module.
func (idx *ModuleIndex) ParseGoMod(filePath string, content []byte) (*ModuleInfo, error) {
	mf, err := modfile.Parse(filePath, content, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod at %s: %w", filePath, err)
	}

	if mf.Module == nil || mf.Module.Mod.Path == "" {
		return nil, fmt.Errorf("go.mod at %s has no module directive", filePath)
	}

	modDir := CleanRepoDir(path.Dir(filePath))
	modPath := strings.TrimSpace(mf.Module.Mod.Path)

	info := &ModuleInfo{
		ModulePath: modPath,
		Dir:        modDir,
		ModFile:    filePath,
		Replaces:   make(map[string]string),
	}

	// Parse replace directives safely (Zero-Execution, local repository references only)
	for _, rep := range mf.Replace {
		oldPath := strings.TrimSpace(rep.Old.Path)
		newPath := strings.TrimSpace(rep.New.Path)

		// We only resolve local directory replacements (starting with ./ or ../)
		if strings.HasPrefix(newPath, "./") || strings.HasPrefix(newPath, "../") {
			// Resolve relative to modDir
			resolvedDir := path.Clean(path.Join(modDir, newPath))
			info.Replaces[oldPath] = resolvedDir
			idx.LocalReplacements[oldPath] = resolvedDir
		}
	}

	idx.ModulesByDir[modDir] = info
	idx.ModulesByPath[modPath] = append(idx.ModulesByPath[modPath], info)
	if len(idx.ModulesByPath[modPath]) > 1 {
		idx.AmbiguousModules[modPath] = true
	}

	return info, nil
}

// ParseGoWork parses a go.work file content to recognize workspace modules.
func (idx *ModuleIndex) ParseGoWork(filePath string, content []byte) error {
	wf, err := modfile.ParseWork(filePath, content, nil)
	if err != nil {
		return fmt.Errorf("parse go.work at %s: %w", filePath, err)
	}

	workDir := CleanRepoDir(path.Dir(filePath))
	for _, use := range wf.Use {
		usePath := strings.TrimSpace(use.Path)
		if strings.HasPrefix(usePath, "/") {
			continue // ignore absolute host paths
		}
		resolvedDir := path.Clean(path.Join(workDir, usePath))
		if strings.HasPrefix(resolvedDir, "..") {
			continue // outside repo
		}
		// If the module was already registered, or to be registered, workDir/use is a valid module root
	}

	for _, rep := range wf.Replace {
		oldPath := strings.TrimSpace(rep.Old.Path)
		newPath := strings.TrimSpace(rep.New.Path)
		if strings.HasPrefix(newPath, "./") || strings.HasPrefix(newPath, "../") {
			resolvedDir := path.Clean(path.Join(workDir, newPath))
			idx.LocalReplacements[oldPath] = resolvedDir
		}
	}

	return nil
}

// FindModuleForDir locates the closest enclosing Go module for a given repo directory.
func (idx *ModuleIndex) FindModuleForDir(dir string) *ModuleInfo {
	cur := CleanRepoDir(dir)
	for {
		if mod, exists := idx.ModulesByDir[cur]; exists {
			return mod
		}
		if cur == "." {
			break
		}
		parent := path.Dir(cur)
		if parent == cur {
			break
		}
		cur = CleanRepoDir(parent)
	}
	return nil
}
