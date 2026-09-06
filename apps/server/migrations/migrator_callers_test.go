package migrations

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	migrationPackagePath = "github.com/dannyota/aboutme/apps/server/migrations"
	goosePackagePath     = "github.com/pressly/goose/v3"
)

func TestMigrationCallerStaticGuard(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		source string
		want   string
	}{
		{
			name: "aliased raw provider",
			file: "cmd/example/main.go",
			source: `package main
import mig "github.com/dannyota/aboutme/apps/server/migrations"
func run() { mig.NewProvider(nil, nil) }
`,
			want: "production caller uses migrations.NewProvider",
		},
		{
			name: "same package raw provider",
			file: "migrations/example.go",
			source: `package migrations
func run() { NewProvider(nil, nil) }
`,
			want: "raw same-package NewProvider call",
		},
		{
			name: "raw goose alias",
			file: "cmd/example/main.go",
			source: `package main
import g "github.com/pressly/goose/v3"
func run() { g.NewProvider(nil, nil) }
`,
			want: "goose.NewProvider outside approved composition",
		},
		{
			name: "raw goose only in helper declaration",
			file: "migrations/migrations.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func NewProvider() { g.NewProvider(nil, nil) }
`,
			want: "",
		},
		{
			name: "raw goose outside helper declaration",
			file: "migrations/migrations.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func helper() { g.NewProvider(nil, nil) }
`,
			want: "goose.NewProvider outside approved composition",
		},
		{
			name: "raw goose exact composition path",
			file: "other/migrator_apply.go",
			source: `package other
import g "github.com/pressly/goose/v3"
func run() { g.NewProvider(nil, nil) }
`,
			want: "goose.NewProvider outside approved composition",
		},
		{
			name: "unsafe Up",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); provider.Up(nil) }
`,
			want: "unsafe Goose provider method Up",
		},
		{
			name: "unsafe UpByOne",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); provider.UpByOne(nil) }
`,
			want: "unsafe Goose provider method UpByOne",
		},
		{
			name: "unsafe UpTo",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); provider.UpTo(nil, 1) }
`,
			want: "unsafe Goose provider method UpTo",
		},
		{
			name: "unsafe HasPending",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); provider.HasPending(nil) }
`,
			want: "unsafe Goose provider method HasPending",
		},
		{
			name: "unsafe GetDBVersion",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); provider.GetDBVersion(nil) }
`,
			want: "unsafe Goose provider method GetDBVersion",
		},
		{
			name: "unsafe Status",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); provider.Status(nil) }
`,
			want: "unsafe Goose provider method Status",
		},
		{
			name: "dot import is rejected",
			file: "cmd/example/main.go",
			source: `package main
import . "github.com/dannyota/aboutme/apps/server/migrations"
func run() { Apply(nil, nil, MigrationIdentity{}) }
`,
			want: "dot import of protected package",
		},
		{
			name: "allowed public API",
			file: "cmd/example/main.go",
			source: `package main
import mig "github.com/dannyota/aboutme/apps/server/migrations"
func run() { mig.Apply(nil, nil, mig.MigrationIdentity{}); mig.Status(nil, nil, mig.MigrationIdentity{}) }
`,
			want: "",
		},
		{
			name: "allowed goose composition methods",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); provider.ApplyVersion(nil, 1, true); provider.ListSources() }
`,
			want: "",
		},
		{
			name: "comments and strings",
			file: "cmd/example/main.go",
			source: `package main
// migrations.NewProvider(nil, nil) and provider.Up(nil) are documentation.
var text = "goose.NewProvider and provider.Status"
`,
			want: "",
		},
		{
			name: "test file excluded",
			file: "migrations/example_test.go",
			source: `package migrations
func run() { NewProvider(nil, nil) }
`,
			want: "",
		},
		{
			name: "unrelated status shadow",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func migration() { provider, _ := g.NewProvider(nil, nil); provider.ApplyVersion(nil, 1, true) }
func unrelated(provider thing) { provider.Status(nil) }
`,
			want: "",
		},
		{
			name: "nested unrelated status shadow",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); { provider := unrelated(); provider.Status(nil) }; provider.ApplyVersion(nil, 1, true) }
`,
			want: "",
		},
		{
			name: "same block genuine provider method",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); provider.Status(nil) }
`,
			want: "unsafe Goose provider method Status",
		},
		{
			name: "discarded constructor does not taint receiver",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { _, _ = g.NewProvider(nil, nil); provider := unrelated(); provider.Status(nil) }
`,
			want: "",
		},
		{
			name: "range shadow then genuine provider",
			file: "migrations/migrator_apply.go",
			source: `package migrations
import g "github.com/pressly/goose/v3"
func run() { provider, _ := g.NewProvider(nil, nil); for provider := range values { provider.Status(nil) }; provider.Up(nil) }
`,
			want: "unsafe Goose provider method Up",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanMigrationSource(tt.file, []byte(tt.source))
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("scanMigrationSource() = %v, want no violations", got)
				}
				return
			}
			for _, violation := range got {
				if strings.Contains(violation, tt.want) {
					return
				}
			}
			t.Fatalf("scanMigrationSource() = %v, want message containing %q", got, tt.want)
		})
	}
}

func TestMigrationProviderStaticGuard(t *testing.T) {
	violations := scanProductionMigrationCallers(t)
	if len(violations) != 0 {
		t.Fatalf("production migration caller violations:\n%s", strings.Join(violations, "\n"))
	}
}

func scanMigrationSource(filename string, source []byte) []string {
	if strings.HasSuffix(filename, "_test.go") {
		return nil
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, filename, source, parser.SkipObjectResolution)
	if err != nil {
		return []string{fmt.Sprintf("%s: parse error: %v", filename, err)}
	}

	migrationAliases, gooseAliases := packageAliases(file)
	violations := make([]string, 0)
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if (path != migrationPackagePath && path != goosePackagePath) || imp.Name == nil || imp.Name.Name != "." {
			continue
		}
		violations = append(violations, fmt.Sprintf("%s:%d: dot import of protected package %q", filename, fileSet.Position(imp.Pos()).Line, path))
	}

	allowedGooseCalls := allowedGooseProviderCalls(file, filename, gooseAliases)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			if file.Name.Name == "migrations" && fun.Name == "NewProvider" {
				violations = append(violations, fmt.Sprintf("%s:%d: raw same-package NewProvider call", filename, fileSet.Position(call.Pos()).Line))
			}
		case *ast.SelectorExpr:
			receiver, ok := fun.X.(*ast.Ident)
			if !ok {
				return true
			}
			if migrationAliases[receiver.Name] && fun.Sel.Name == "NewProvider" {
				violations = append(violations, fmt.Sprintf("%s:%d: production caller uses migrations.NewProvider", filename, fileSet.Position(call.Pos()).Line))
			}
			if gooseAliases[receiver.Name] && fun.Sel.Name == "NewProvider" && !allowedGooseCalls[call.Pos()] {
				violations = append(violations, fmt.Sprintf("%s:%d: goose.NewProvider outside approved composition", filename, fileSet.Position(call.Pos()).Line))
			}
		}
		return true
	})
	violations = append(violations, gooseProviderViolations(file, filename, gooseAliases, fileSet)...)
	return violations
}

func packageAliases(file *ast.File) (map[string]bool, map[string]bool) {
	migrationAliases := make(map[string]bool)
	gooseAliases := make(map[string]bool)
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || imp.Name != nil && (imp.Name.Name == "_" || imp.Name.Name == ".") {
			continue
		}
		alias := filepath.Base(path)
		if path == goosePackagePath {
			alias = "goose"
		}
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		switch path {
		case migrationPackagePath:
			migrationAliases[alias] = true
		case goosePackagePath:
			gooseAliases[alias] = true
		}
	}
	return migrationAliases, gooseAliases
}

func gooseProviderViolations(file *ast.File, filename string, gooseAliases map[string]bool, fileSet *token.FileSet) []string {
	info := &types.Info{Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object)}
	checker := types.Config{Error: func(error) {}}
	_, checkErr := checker.Check(file.Name.Name, fileSet, []*ast.File{file}, info)
	// Isolated snippets may have unresolved imports or placeholder types;
	// Defs/Uses for local bindings remain the only required result.
	_ = checkErr
	providers := make(map[types.Object]bool)
	violations := make([]string, 0)
	objectFor := func(ident *ast.Ident) types.Object {
		if object := info.Defs[ident]; object != nil {
			return object
		}
		return info.Uses[ident]
	}
	markConstructor := func(lhs ast.Expr, constructor ast.Expr) {
		if !isGooseNewProviderExpr(constructor, gooseAliases) {
			return
		}
		ident, ok := lhs.(*ast.Ident)
		if !ok || ident.Name == "_" {
			return
		}
		object := objectFor(ident)
		if object == nil {
			violations = append(violations, fmt.Sprintf("%s:%d: unresolved Goose provider binding", filename, fileSet.Position(ident.Pos()).Line))
			return
		}
		providers[object] = true
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for i, rhs := range n.Rhs {
				if i < len(n.Lhs) {
					markConstructor(n.Lhs[i], rhs)
				}
			}
		case *ast.ValueSpec:
			for i, value := range n.Values {
				if i < len(n.Names) {
					markConstructor(n.Names[i], value)
				}
			}
		case *ast.CallExpr:
			selector, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || !unsafeGooseProviderMethod(selector.Sel.Name) {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if ok && providers[info.Uses[receiver]] {
				violations = append(violations, fmt.Sprintf("%s:%d: unsafe Goose provider method %s", filename, fileSet.Position(n.Pos()).Line, selector.Sel.Name))
			}
		}
		return true
	})
	return violations
}

func isGooseNewProviderExpr(expression ast.Expr, gooseAliases map[string]bool) bool {
	call, ok := expression.(*ast.CallExpr)
	return ok && isGooseNewProvider(call, gooseAliases)
}

func unsafeGooseProviderMethod(name string) bool {
	switch name {
	case "Up", "UpByOne", "UpTo", "HasPending", "GetDBVersion", "Status":
		return true
	default:
		return false
	}
}

func allowedGooseProviderCalls(file *ast.File, filename string, gooseAliases map[string]bool) map[token.Pos]bool {
	allowed := make(map[token.Pos]bool)
	if file.Name.Name != "migrations" {
		return allowed
	}
	if filename == "migrations/migrator_apply.go" {
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && isGooseNewProvider(call, gooseAliases) {
				allowed[call.Pos()] = true
			}
			return true
		})
		return allowed
	}
	if filename != "migrations/migrations.go" {
		return allowed
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "NewProvider" || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && isGooseNewProvider(call, gooseAliases) {
				allowed[call.Pos()] = true
			}
			return true
		})
	}
	return allowed
}

func isGooseNewProvider(call *ast.CallExpr, gooseAliases map[string]bool) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "NewProvider" {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	return ok && gooseAliases[receiver.Name]
}

func scanProductionMigrationCallers(t *testing.T) []string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	serverRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), ".."))
	var violations []string
	err := filepath.Walk(serverRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(serverRoot, path)
		if err != nil {
			return err
		}
		violations = append(violations, scanMigrationSource(filepath.ToSlash(relativePath), source)...)
		return nil
	})
	if err != nil {
		t.Fatalf("scan production migration callers: %v", err)
	}
	sort.Strings(violations)
	return violations
}
