package python

import (
	"path"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

// ResolveImport resolves a single extracted Python import from sourcePath against the ModuleIndex.
// It strictly assigns internal only when a single target file in the repository is unambiguously matched.
func ResolveImport(sourcePath string, imp RawImport, idx *ModuleIndex) domain.ImportReference {
	ref := domain.ImportReference{
		SourcePath: sourcePath,
		ImportText: imp.ImportText,
		ModuleName: formatModuleName(imp),
		LineNumber: imp.LineNumber,
	}

	if imp.Level > 0 {
		target, note, ok := resolveRelativeImport(sourcePath, imp, idx)
		if ok {
			ref.Classification = domain.ImportInternal
			ref.TargetPath = target
			return ref
		}
		ref.Classification = domain.ImportUnresolved
		ref.ResolutionNote = strPtr(note)
		return ref
	}

	// Absolute import (Level == 0)
	if imp.Module == "" {
		ref.Classification = domain.ImportUnresolved
		ref.ResolutionNote = strPtr("empty module name")
		return ref
	}

	// Case 1: "from M import name" -> prefer M.name if it uniquely resolves to a module/package
	if imp.ImportedName != "" && imp.ImportedName != "*" {
		subCandidate := imp.Module + "." + imp.ImportedName
		if target, ambiguous, found := idx.LookupModule(subCandidate); found {
			if ambiguous {
				ref.Classification = domain.ImportUnresolved
				ref.ResolutionNote = strPtr("ambiguous internal module: " + subCandidate)
				return ref
			}
			ref.Classification = domain.ImportInternal
			ref.TargetPath = target
			return ref
		}
	}

	// Case 2: Resolve M itself ("import M" or fallback for "from M import symbol")
	if target, ambiguous, found := idx.LookupModule(imp.Module); found {
		if ambiguous {
			ref.Classification = domain.ImportUnresolved
			ref.ResolutionNote = strPtr("ambiguous internal module: " + imp.Module)
			return ref
		}
		ref.Classification = domain.ImportInternal
		ref.TargetPath = target
		return ref
	}

	// Case 3: Not found in ModuleIndex. Check if top-level package belongs to this repository.
	top := topSegment(imp.Module)
	if idx.IsInternalTopPackage(top) {
		ref.Classification = domain.ImportUnresolved
		ref.ResolutionNote = strPtr("unresolved internal module: " + imp.Module)
		return ref
	}

	ref.Classification = domain.ImportExternal
	return ref
}

func resolveRelativeImport(sourcePath string, imp RawImport, idx *ModuleIndex) (string, string, bool) {
	// Determine root boundary ("src" if file is under "src/", else "")
	rootPrefix := ""
	relWithinRoot := sourcePath
	if strings.HasPrefix(sourcePath, "src/") {
		rootPrefix = "src"
		relWithinRoot = strings.TrimPrefix(sourcePath, "src/")
	}

	dir := path.Dir(relWithinRoot)
	if dir == "." {
		dir = ""
	}

	var dirParts []string
	if dir != "" {
		dirParts = strings.Split(dir, "/")
	}

	// Level 1 (".") stays in dirParts; Level 2 ("..") goes up 1 directory, etc.
	upCount := imp.Level - 1
	if upCount > len(dirParts) {
		return "", "relative import goes beyond top-level package", false
	}
	baseParts := dirParts[:len(dirParts)-upCount]

	var fullBaseParts []string
	if rootPrefix != "" {
		fullBaseParts = append(fullBaseParts, rootPrefix)
	}
	fullBaseParts = append(fullBaseParts, baseParts...)
	baseDir := strings.Join(fullBaseParts, "/")

	modRelPath := ""
	if imp.Module != "" {
		modRelPath = strings.ReplaceAll(imp.Module, ".", "/")
	}

	// Subcase A: "from .mod import name" or "from . import name"
	if imp.ImportedName != "" && imp.ImportedName != "*" {
		var subDir string
		if modRelPath != "" {
			subDir = joinRel(baseDir, modRelPath, strings.ReplaceAll(imp.ImportedName, ".", "/"))
		} else {
			subDir = joinRel(baseDir, strings.ReplaceAll(imp.ImportedName, ".", "/"))
		}
		if target, ambiguous, found := lookupPathAsModuleOrPackage(subDir, idx); found {
			if ambiguous {
				return "", "ambiguous relative submodule match", false
			}
			return target, "", true
		}
	}

	// Subcase B: If Module is specified (e.g. "from .mod import Symbol" or "from .mod import *"),
	// fall back to resolving ".mod" as a module or package.
	if modRelPath != "" {
		modTargetDir := joinRel(baseDir, modRelPath)
		if target, ambiguous, found := lookupPathAsModuleOrPackage(modTargetDir, idx); found {
			if ambiguous {
				return "", "ambiguous relative module match", false
			}
			return target, "", true
		}
		return "", "relative module target not found", false
	}

	// Subcase C: "from . import *"
	if imp.ImportedName == "*" {
		initCandidate := joinRel(baseDir, "__init__.py")
		if idx.HasFile(initCandidate) {
			return initCandidate, "", true
		}
	}

	return "", "relative import target not found", false
}

func lookupPathAsModuleOrPackage(basePath string, idx *ModuleIndex) (string, bool, bool) {
	if basePath == "" {
		initFile := "__init__.py"
		if idx.HasFile(initFile) {
			return initFile, false, true
		}
		return "", false, false
	}
	pyFile := basePath + ".py"
	initFile := basePath + "/__init__.py"
	hasPy := idx.HasFile(pyFile)
	hasInit := idx.HasFile(initFile)

	if hasPy && hasInit {
		return "", true, true
	}
	if hasPy {
		return pyFile, false, true
	}
	if hasInit {
		return initFile, false, true
	}
	return "", false, false
}

func joinRel(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		trimmed := strings.Trim(p, "/")
		if trimmed != "" {
			nonEmpty = append(nonEmpty, trimmed)
		}
	}
	return strings.Join(nonEmpty, "/")
}

func formatModuleName(imp RawImport) string {
	prefix := strings.Repeat(".", imp.Level)
	if imp.Module != "" && imp.ImportedName != "" {
		return prefix + imp.Module + "." + imp.ImportedName
	}
	if imp.Module != "" {
		return prefix + imp.Module
	}
	if imp.ImportedName != "" {
		return prefix + imp.ImportedName
	}
	return prefix
}

func strPtr(s string) *string {
	return &s
}
