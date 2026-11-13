package printsnapshot

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const printsnapshotImport = "danny.vn/aboutme/apps/server/internal/printsnapshot"

func envelopeLinkWrites(root string) ([]string, error) {
	var writes []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == "testdata" || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		for _, position := range fileLinkWrites(file) {
			writes = append(writes, fmt.Sprintf("%s:%d", path, fileSet.Position(position).Line))
		}
		return nil
	})
	return writes, err
}

func fileLinkWrites(file *ast.File) []token.Pos {
	inside := file.Name.Name == "printsnapshot"
	alias := ""
	for _, spec := range file.Imports {
		if path, err := strconv.Unquote(spec.Path.Value); err == nil && path == printsnapshotImport {
			alias = "printsnapshot"
			if spec.Name != nil {
				alias = spec.Name.Name
			}
		}
	}
	envelopeType := func(expression ast.Expr) bool {
		switch typed := expression.(type) {
		case *ast.Ident:
			return inside && typed.Name == "Envelope"
		case *ast.SelectorExpr:
			qualifier, ok := typed.X.(*ast.Ident)
			return ok && alias != "" && qualifier.Name == alias && typed.Sel.Name == "Envelope"
		}
		return false
	}
	links := func(expression ast.Expr) bool {
		if index, ok := expression.(*ast.IndexExpr); ok {
			expression = index.X
		}
		selector, ok := expression.(*ast.SelectorExpr)
		return ok && selector.Sel.Name == "ProjectLinks"
	}
	guarded := inside || alias != ""
	var writes []token.Pos
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncDecl:
			return !inside || typed.Recv != nil || typed.Name.Name != "WithProjectLinks"
		case *ast.CompositeLit:
			if envelopeType(typed.Type) {
				for _, element := range typed.Elts {
					pair, keyed := element.(*ast.KeyValueExpr)
					if !keyed {
						writes = append(writes, typed.Pos())
						break
					}
					if key, ok := pair.Key.(*ast.Ident); ok && key.Name == "ProjectLinks" {
						writes = append(writes, typed.Pos())
						break
					}
				}
			}
		case *ast.AssignStmt:
			for _, target := range typed.Lhs {
				if guarded && links(target) {
					writes = append(writes, target.Pos())
				}
			}
		case *ast.CallExpr:
			name, ok := typed.Fun.(*ast.Ident)
			if guarded && ok && (name.Name == "delete" || name.Name == "clear") && len(typed.Args) != 0 && links(typed.Args[0]) {
				writes = append(writes, typed.Pos())
			}
		case *ast.UnaryExpr:
			if guarded && typed.Op == token.AND && links(typed.X) {
				writes = append(writes, typed.Pos())
			}
		}
		return true
	})
	return writes
}

func TestOnlyWithProjectLinksSetsTheEnvelopeLinks(t *testing.T) {
	writes, err := envelopeLinkWrites(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 0 {
		t.Fatalf("code outside WithProjectLinks sets Envelope.ProjectLinks: %v", writes)
	}
}

func TestEnvelopeLinkWritesFindsEachKindOfWrite(t *testing.T) {
	writes, err := envelopeLinkWrites(filepath.Join("testdata", "linkwrites"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join("testdata", "linkwrites", "inside", "inside.go") + ":9",
		filepath.Join("testdata", "linkwrites", "inside", "inside.go") + ":14",
		filepath.Join("testdata", "linkwrites", "outside", "outside.go") + ":8",
		filepath.Join("testdata", "linkwrites", "outside", "outside.go") + ":12",
		filepath.Join("testdata", "linkwrites", "outside", "outside.go") + ":16",
		filepath.Join("testdata", "linkwrites", "outside", "outside.go") + ":20",
		filepath.Join("testdata", "linkwrites", "outside", "outside.go") + ":24",
	}
	slices.Sort(writes)
	slices.Sort(want)
	if !slices.Equal(writes, want) {
		t.Fatalf("writes = %v, want %v", writes, want)
	}
}
