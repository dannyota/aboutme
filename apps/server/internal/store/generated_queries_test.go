package store_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var showcaseReviewObjects = regexp.MustCompile(`\b(review_key|reviewed_key|review_outcome|reviewed_at|first_listed_at|resume_showcase_listing_idx)\b`)

func generatedStatements(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("*.sql.go")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no generated query files: %v", err)
	}
	statements := map[string]string{}
	files := token.NewFileSet()
	for _, path := range paths {
		file, parseErr := parser.ParseFile(files, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		for _, decl := range file.Decls {
			gen, isGen := decl.(*ast.GenDecl)
			if !isGen || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, isValue := spec.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				for i, name := range value.Names {
					if i >= len(value.Values) {
						continue
					}
					literal, isLiteral := value.Values[i].(*ast.BasicLit)
					if !isLiteral || literal.Kind != token.STRING {
						continue
					}
					text, unquoteErr := strconv.Unquote(literal.Value)
					if unquoteErr != nil {
						t.Fatalf("unquote %s in %s: %v", name.Name, path, unquoteErr)
					}
					statements[path+": "+name.Name] = text
				}
			}
		}
	}
	return statements
}

func TestGeneratedQueries_NameNoShowcaseReviewColumns(t *testing.T) {
	t.Parallel()
	showcase := 0
	for name, text := range generatedStatements(t) {
		if strings.Contains(text, "resume_showcase") {
			showcase++
		}
		if match := showcaseReviewObjects.FindString(text); match != "" {
			t.Errorf("%s names %s", name, match)
		}
	}
	if showcase == 0 {
		t.Fatal("found no generated statement on resume_showcase")
	}
}
