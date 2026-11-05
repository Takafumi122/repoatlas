package golang_test

import (
	"context"
	"strings"
	"testing"

	"github.com/repoatlas/repoatlas/backend/internal/analyzer/golang"
	"github.com/repoatlas/repoatlas/backend/internal/domain"
)

func TestGoAnalyzer_SyntheticSingleModule(t *testing.T) {
	files := []golang.SourceInput{
		{
			Path: "go.mod",
			Content: []byte(`module github.com/example/myapp

go 1.25.0
`),
		},
		{
			Path: "cmd/server/main.go",
			Content: []byte(`package main

import (
	"fmt"
	"net/http"
	"github.com/example/myapp/internal/api"
)

func main() {
	fmt.Println("Starting server...")
	api.StartServer()
}
`),
		},
		{
			Path: "internal/api/handler.go",
			Content: []byte(`package api

import (
	"context"
	"github.com/example/myapp/internal/service"
)

type Handler struct {
	svc *service.UserService
}

func NewHandler(s *service.UserService) *Handler {
	return &Handler{svc: s}
}

func (h *Handler) ServeHTTP(ctx context.Context) error {
	return h.svc.DoWork(ctx)
}

func StartServer() {}
`),
		},
		{
			Path: "internal/api/routes.go",
			Content: []byte(`package api

import (
	"github.com/example/myapp/internal/service"
)

func RegisterRoutes(h *Handler, s *service.UserService) {}
`),
		},
		{
			Path: "internal/service/user.go",
			Content: []byte(`//go:build !windows
package service

import (
	"context"
	_ "database/sql"
	svc "github.com/example/myapp/internal/store"
)

type UserService struct {
	st svc.Store
}

func NewUserService(st svc.Store) *UserService {
	return &UserService{st: st}
}

func (s *UserService) DoWork(ctx context.Context) error {
	return s.st.Save(ctx)
}
`),
		},
		{
			Path: "internal/store/postgres.go",
			Content: []byte(`package store

import (
	"context"
	"errors"
)

type Store interface {
	Save(ctx context.Context) error
}

type PostgresStore struct{}

func (p *PostgresStore) Save(ctx context.Context) error {
	return nil
}
`),
		},
	}

	az := golang.NewGoAnalyzer()
	out, err := az.Analyze(context.Background(), files, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if out.Language != "go" {
		t.Errorf("expected Language == 'go', got %s", out.Language)
	}
	if out.GraphGranularity != "package" {
		t.Errorf("expected GraphGranularity == 'package', got %s", out.GraphGranularity)
	}

	// 4 packages: cmd/server, internal/api, internal/service, internal/store
	if len(out.Files) != 4 {
		t.Fatalf("expected 4 package nodes, got %d", len(out.Files))
	}

	pkgMap := make(map[string]domain.SourceFile)
	for _, f := range out.Files {
		pkgMap[f.PackageImportPath] = f
	}

	serverPkg, ok := pkgMap["github.com/example/myapp/cmd/server"]
	if !ok {
		t.Fatalf("missing cmd/server package")
	}
	if !serverPkg.IsEntrypoint {
		t.Errorf("expected cmd/server to be entrypoint candidate")
	}
	if serverPkg.PackageName != "main" {
		t.Errorf("expected cmd/server package_name == main, got %s", serverPkg.PackageName)
	}

	apiPkg, ok := pkgMap["github.com/example/myapp/internal/api"]
	if !ok {
		t.Fatalf("missing internal/api package")
	}
	// internal/api has 2 files: handler.go and routes.go
	if apiPkg.FilesCount != 2 {
		t.Errorf("expected 2 files in internal/api, got %d", apiPkg.FilesCount)
	}
	// handler.go has NewHandler, StartServer (2 funcs) + ServeHTTP (1 method) + Handler (1 type)
	// routes.go has RegisterRoutes (1 func)
	if apiPkg.FunctionsCount != 3 {
		t.Errorf("expected 3 functions in internal/api, got %d", apiPkg.FunctionsCount)
	}
	if apiPkg.MethodsCount != 1 {
		t.Errorf("expected 1 method in internal/api, got %d", apiPkg.MethodsCount)
	}
	if apiPkg.TypesCount != 1 {
		t.Errorf("expected 1 type in internal/api, got %d", apiPkg.TypesCount)
	}

	// Verify edges:
	// cmd/server -> internal/api
	// internal/api -> internal/service (from both handler.go and routes.go, but aggregated to 1 edge!)
	// internal/service -> internal/store
	edgeMap := make(map[string]bool)
	for _, e := range out.Edges {
		edgeMap[e.SourcePath+" -> "+e.TargetPath] = true
	}

	expectedEdges := []string{
		"github.com/example/myapp/cmd/server -> github.com/example/myapp/internal/api",
		"github.com/example/myapp/internal/api -> github.com/example/myapp/internal/service",
		"github.com/example/myapp/internal/service -> github.com/example/myapp/internal/store",
	}

	for _, exp := range expectedEdges {
		if !edgeMap[exp] {
			t.Errorf("missing expected edge: %s", exp)
		}
	}

	if len(out.Edges) != 3 {
		t.Errorf("expected exactly 3 deduplicated package edges, got %d", len(out.Edges))
	}

	// Verify import references evidence
	var serviceImports []domain.ImportReference
	for _, imp := range out.Imports {
		if imp.SourcePackage == "github.com/example/myapp/internal/service" {
			serviceImports = append(serviceImports, imp)
		}
	}

	var hasBlankSQL, hasAliasedStore bool
	for _, imp := range serviceImports {
		if imp.ModuleName == "database/sql" && imp.Alias == "_" && imp.Classification == domain.ImportExternal {
			hasBlankSQL = true
		}
		if imp.TargetPackage == "github.com/example/myapp/internal/store" && imp.Alias == "svc" && imp.Classification == domain.ImportInternal {
			hasAliasedStore = true
		}
		if imp.BuildConstraints != "!windows" {
			t.Errorf("expected build constraint '!windows', got %q", imp.BuildConstraints)
		}
	}

	if !hasBlankSQL {
		t.Errorf("expected blank import database/sql to be recorded")
	}
	if !hasAliasedStore {
		t.Errorf("expected aliased import svc internal/store to be recorded")
	}
}

func TestGoAnalyzer_MultiModuleAndWorkAndReplace(t *testing.T) {
	files := []golang.SourceInput{
		{
			Path: "go.work",
			Content: []byte(`go 1.25.0

use (
	./app
	./pkg/util
)
`),
		},
		{
			Path: "app/go.mod",
			Content: []byte(`module github.com/example/app

go 1.25.0

replace github.com/example/util => ../pkg/util
`),
		},
		{
			Path: "app/main.go",
			Content: []byte(`package main

import (
	"fmt"
	"github.com/example/util"
)

func main() {
	fmt.Println(util.Helper())
}
`),
		},
		{
			Path: "pkg/util/go.mod",
			Content: []byte(`module github.com/example/util

go 1.25.0
`),
		},
		{
			Path: "pkg/util/helper.go",
			Content: []byte(`package util

func Helper() string {
	return "ok"
}
`),
		},
	}

	az := golang.NewGoAnalyzer()
	out, err := az.Analyze(context.Background(), files, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(out.Files) != 2 {
		t.Fatalf("expected 2 packages across 2 modules, got %d", len(out.Files))
	}

	// Verify replace resolved github.com/example/util to internal package
	foundEdge := false
	for _, e := range out.Edges {
		if e.SourcePath == "github.com/example/app" && e.TargetPath == "github.com/example/util" {
			foundEdge = true
			break
		}
	}
	if !foundEdge {
		t.Errorf("expected internal edge from app to util via local replace")
	}
}

func TestGoAnalyzer_AmbiguousModuleHandling(t *testing.T) {
	files := []golang.SourceInput{
		{
			Path: "mod1/go.mod",
			Content: []byte(`module github.com/example/conflict
go 1.25.0
`),
		},
		{
			Path: "mod1/foo.go",
			Content: []byte(`package conflict
func F1() {}
`),
		},
		{
			Path: "mod2/go.mod",
			Content: []byte(`module github.com/example/conflict
go 1.25.0
`),
		},
		{
			Path: "mod2/bar.go",
			Content: []byte(`package conflict
func F2() {}
`),
		},
		{
			Path: "caller/go.mod",
			Content: []byte(`module github.com/example/caller
go 1.25.0
`),
		},
		{
			Path: "caller/main.go",
			Content: []byte(`package main
import "github.com/example/conflict"
func main() {}
`),
		},
	}

	az := golang.NewGoAnalyzer()
	out, err := az.Analyze(context.Background(), files, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// The import "github.com/example/conflict" should NOT be resolved as internal edge due to ambiguity
	for _, e := range out.Edges {
		if e.TargetPath == "github.com/example/conflict" {
			t.Errorf("ambiguous module should not produce internal edge")
		}
	}

	// Check unresolved imports count
	if out.UnresolvedImportsCount < 1 {
		t.Errorf("expected unresolved import count >= 1 for ambiguous module, got %d", out.UnresolvedImportsCount)
	}
}

func TestGoAnalyzer_OutOfRepoReplace_Unresolved(t *testing.T) {
	files := []golang.SourceInput{
		{
			Path: "go.mod",
			Content: []byte(`module github.com/example/replacetest
go 1.22
replace github.com/secret/lib => ../../outside_repo
`),
		},
		{
			Path: "main.go",
			Content: []byte(`package main
import "github.com/secret/lib"
func main() {}
`),
		},
	}

	az := golang.NewGoAnalyzer()
	out, err := az.Analyze(context.Background(), files, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if out.UnresolvedImportsCount != 1 {
		t.Errorf("expected 1 unresolved import for out-of-repo replace, got %d", out.UnresolvedImportsCount)
	}
	if len(out.Imports) != 1 || out.Imports[0].Classification != domain.ImportUnresolved {
		t.Errorf("expected import to be classified as unresolved: %+v", out.Imports)
	}
}

func TestGoAnalyzer_MutuallyExclusiveBuildConstraints(t *testing.T) {
	files := []golang.SourceInput{
		{
			Path: "go.mod",
			Content: []byte(`module github.com/example/buildtagtest
go 1.22
`),
		},
		{
			Path: "main.go",
			Content: []byte(`package main
import "github.com/example/buildtagtest/osutil"
func main() {}
`),
		},
		{
			Path: "osutil/os_linux.go",
			Content: []byte(`//go:build linux
package osutil
import "golang.org/x/sys/unix"
func Syscall() {}
`),
		},
		{
			Path: "osutil/os_windows.go",
			Content: []byte(`//go:build windows
package osutil
import "golang.org/x/sys/windows"
func Syscall() {}
`),
		},
	}

	az := golang.NewGoAnalyzer()
	out, err := az.Analyze(context.Background(), files, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(out.Files) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(out.Files))
	}

	// Verify build constraints were extracted on imports
	var foundLinux, foundWindows bool
	for _, imp := range out.Imports {
		if strings.Contains(imp.BuildConstraints, "linux") {
			foundLinux = true
		}
		if strings.Contains(imp.BuildConstraints, "windows") {
			foundWindows = true
		}
	}
	if !foundLinux || !foundWindows {
		t.Errorf("expected build constraints 'linux' and 'windows' to be recorded: foundLinux=%v foundWindows=%v", foundLinux, foundWindows)
	}
}
