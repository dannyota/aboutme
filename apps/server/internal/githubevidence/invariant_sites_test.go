package githubevidence

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	notificationMethod = "AccountSessionsEndedLocked"
	boundaryMethod     = "inAccount"
)

func serverRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "internal", "githubevidence", "service.go")); err != nil {
		t.Fatalf("server root %s: %v", root, err)
	}
	return root
}

func notificationSites(root string) (map[invariantSite]bool, error) {
	sites := map[invariantSite]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			owner, enclosing := "", ""
			if fn, ok := decl.(*ast.FuncDecl); ok {
				owner, enclosing = fn.Name.Name, declName(fn)
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				sel, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				notifies := sel.Sel.Name == notificationMethod && owner != notificationMethod
				enters := sel.Sel.Name == boundaryMethod && file.Name.Name == "githubevidence"
				if notifies || enters {
					sites[invariantSite{file: filepath.ToSlash(rel), fn: enclosing}] = true
				}
				return true
			})
		}
		return nil
	})
	return sites, err
}

func declName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	switch typed := recv.(type) {
	case *ast.IndexExpr:
		recv = typed.X
	case *ast.IndexListExpr:
		recv = typed.X
	}
	if ident, ok := recv.(*ast.Ident); ok {
		return ident.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

func TestInvariantSites_TheTableNamesEveryNotificationSite(t *testing.T) {
	found, err := notificationSites(serverRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("found no call to AccountSessionsEndedLocked or inAccount under apps/server")
	}
	named := map[invariantSite]bool{}
	for _, tc := range callerCases() {
		named[tc.site] = true
		if tc.run == nil || tc.invalidate == nil || len(tc.lastLocks) == 0 || tc.lapse == nil {
			t.Errorf("case %q lacks an I1, I2 or I3 step", tc.name)
		}
	}
	var missing, gone []string
	for site := range found {
		if !named[site] {
			missing = append(missing, site.String())
		}
	}
	for site := range named {
		if !found[site] {
			gone = append(gone, site.String())
		}
	}
	slices.Sort(missing)
	slices.Sort(gone)
	for _, site := range missing {
		t.Errorf("%s ends account sessions or enters the evidence boundary, but no invariant case names it", site)
	}
	for _, site := range gone {
		t.Errorf("an invariant case names %s, which no longer ends account sessions or enters the evidence boundary", site)
	}
}
