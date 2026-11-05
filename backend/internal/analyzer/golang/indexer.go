package golang

import (
	"path"
	"sort"
	"strings"

	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

// PackageData represents an indexed Go package within the repository.
type PackageData struct {
	ImportPath     string // e.g. "github.com/example/myapp/cmd/server"
	Directory      string // e.g. "cmd/server"
	PackageName    string // e.g. "main"
	ModulePath     string // e.g. "github.com/example/myapp"
	Files          []ParsedGoFile
	FunctionsCount int
	MethodsCount   int
	TypesCount     int
	IsEntrypoint   bool
}

// PackageIndex indexes all Go packages and resolves import paths.
type PackageIndex struct {
	ModIndex         *ModuleIndex
	PackagesByPath   map[string]*PackageData // import path -> package
	PackagesByDir    map[string]*PackageData // directory -> package
	ImportPathToDir  map[string]string       // import path -> directory
	DirToImportPath  map[string]string       // directory -> import path
}

// BuildPackageIndex groups parsed Go files by directory and maps each directory to a package import path.
func BuildPackageIndex(modIndex *ModuleIndex, files []ParsedGoFile) *PackageIndex {
	pkgIndex := &PackageIndex{
		ModIndex:        modIndex,
		PackagesByPath:  make(map[string]*PackageData),
		PackagesByDir:   make(map[string]*PackageData),
		ImportPathToDir: make(map[string]string),
		DirToImportPath: make(map[string]string),
	}

	// 1. Group files by directory
	filesByDir := make(map[string][]ParsedGoFile)
	for _, f := range files {
		dir := CleanRepoDir(path.Dir(f.Path))
		filesByDir[dir] = append(filesByDir[dir], f)
	}

	// 2. Identify package name and package import path for each directory
	var allDirs []string
	for dir := range filesByDir {
		allDirs = append(allDirs, dir)
	}
	sort.Strings(allDirs)

	for _, dir := range allDirs {
		dirFiles := filesByDir[dir]
		if len(dirFiles) == 0 {
			continue
		}

		// Determine package name: prefer non-test files, otherwise test file
		pkgName := ""
		for _, f := range dirFiles {
			if !f.IsTest && f.PackageName != "" {
				pkgName = f.PackageName
				break
			}
		}
		if pkgName == "" && len(dirFiles) > 0 {
			pkgName = dirFiles[0].PackageName
		}
		if pkgName == "" {
			pkgName = path.Base(dir)
			if pkgName == "." {
				pkgName = "main"
			}
		}

		// Find enclosing module
		mod := modIndex.FindModuleForDir(dir)
		var importPath string
		var modPath string

		if mod != nil {
			modPath = mod.ModulePath
			var relFromMod string
			if mod.Dir == "." {
				relFromMod = dir
			} else if dir == mod.Dir {
				relFromMod = "."
			} else {
				relFromMod = strings.TrimPrefix(dir, mod.Dir+"/")
			}

			if relFromMod == "." || relFromMod == "" {
				importPath = modPath
			} else {
				importPath = modPath + "/" + relFromMod
			}
		} else {
			// Fallback: if no go.mod exists in repository, use directory-based synthetic import path
			modPath = "repo"
			if dir == "." {
				importPath = "repo"
			} else {
				importPath = "repo/" + dir
			}
		}

		var totalFuncs, totalMethods, totalTypes int
		for _, f := range dirFiles {
			totalFuncs += f.FunctionsCount
			totalMethods += f.MethodsCount
			totalTypes += f.TypesCount
		}

		isEntry := pkgName == "main" || dir == "cmd" || strings.HasPrefix(dir, "cmd/")

		pkgData := &PackageData{
			ImportPath:     importPath,
			Directory:      dir,
			PackageName:    pkgName,
			ModulePath:     modPath,
			Files:          dirFiles,
			FunctionsCount: totalFuncs,
			MethodsCount:   totalMethods,
			TypesCount:     totalTypes,
			IsEntrypoint:   isEntry,
		}

		pkgIndex.PackagesByPath[importPath] = pkgData
		pkgIndex.PackagesByDir[dir] = pkgData
		pkgIndex.ImportPathToDir[importPath] = dir
		pkgIndex.DirToImportPath[dir] = importPath
	}

	return pkgIndex
}

// ResolutionResult represents the outcome of resolving an import path.
type ResolutionResult struct {
	Classification domain.ImportClassification
	TargetPackage  string // resolved package import path if internal
	TargetDir      string // resolved directory if internal
	Reason         string
}

// IsStandardLibrary checks if an import path belongs to the Go standard library.
// A Go import path without a '.' in its first segment is part of the standard library (or toolchain).
func IsStandardLibrary(importPath string) bool {
	if importPath == "" || importPath == "C" {
		return true
	}
	firstSlash := strings.Index(importPath, "/")
	var firstSegment string
	if firstSlash == -1 {
		firstSegment = importPath
	} else {
		firstSegment = importPath[:firstSlash]
	}
	return !strings.Contains(firstSegment, ".")
}

// ResolveImport resolves an import path against the indexed packages, replace rules, and modules.
func (pi *PackageIndex) ResolveImport(sourcePkg *PackageData, imp ParsedImport) ResolutionResult {
	rawPath := strings.TrimSpace(imp.Path)
	if rawPath == "" {
		return ResolutionResult{
			Classification: domain.ImportUnresolved,
			Reason:         "Empty import path",
		}
	}

	if rawPath == "C" {
		return ResolutionResult{
			Classification: domain.ImportExternal,
			Reason:         "cgo pseudo-package",
		}
	}

	// 1. Check local replace mappings (e.g. replace old.com/pkg => ./internal/pkg)
	for oldPrefix, targetDir := range pi.ModIndex.LocalReplacements {
		if rawPath == oldPrefix || strings.HasPrefix(rawPath, oldPrefix+"/") {
			sub := strings.TrimPrefix(rawPath, oldPrefix)
			sub = strings.TrimPrefix(sub, "/")
			resolvedDir := path.Clean(path.Join(targetDir, sub))
			if targetPkg, ok := pi.PackagesByDir[resolvedDir]; ok {
				return ResolutionResult{
					Classification: domain.ImportInternal,
					TargetPackage:  targetPkg.ImportPath,
					TargetDir:      targetPkg.Directory,
					Reason:         "Resolved via local replace directive",
				}
			}
			return ResolutionResult{
				Classification: domain.ImportUnresolved,
				Reason:         "Local replace target directory is missing or outside repository",
			}
		}
	}

	// 2. Exact match in internal packages index
	if targetPkg, ok := pi.PackagesByPath[rawPath]; ok {
		// Verify not ambiguous module
		if pi.ModIndex.AmbiguousModules[targetPkg.ModulePath] {
			return ResolutionResult{
				Classification: domain.ImportUnresolved,
				Reason:         "Ambiguous module path defined in multiple directories",
			}
		}
		return ResolutionResult{
			Classification: domain.ImportInternal,
			TargetPackage:  targetPkg.ImportPath,
			TargetDir:      targetPkg.Directory,
			Reason:         "Resolved to internal repository package",
		}
	}

	// 3. Standard library check
	if IsStandardLibrary(rawPath) {
		return ResolutionResult{
			Classification: domain.ImportExternal,
			Reason:         "Go standard library",
		}
	}

	// 4. External third-party import
	return ResolutionResult{
		Classification: domain.ImportExternal,
		Reason:         "External third-party module",
	}
}
