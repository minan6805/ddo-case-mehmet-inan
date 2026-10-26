package httpapi

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Check the embedded contract against actual router registrations, including
// routes registered in loops (merge/split and dependency PUT/DELETE).
func TestOpenAPICoversRegisteredRoutes(t *testing.T) {
	var spec struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(openAPIDocument, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.0.3" {
		t.Fatalf("unexpected version %q", spec.OpenAPI)
	}
	documented := map[string]bool{}
	for path, methods := range spec.Paths {
		for method := range methods {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}
	registered := map[string]bool{}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		tree, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var visit func(ast.Node, map[string]string)
		var value func(ast.Expr, map[string]string) (string, bool)
		value = func(expr ast.Expr, env map[string]string) (string, bool) {
			switch e := expr.(type) {
			case *ast.BasicLit:
				s, err := strconv.Unquote(e.Value)
				return s, err == nil
			case *ast.Ident:
				s, ok := env[e.Name]
				return s, ok
			case *ast.BinaryExpr:
				if e.Op != token.ADD {
					return "", false
				}
				a, ok := value(e.X, env)
				b, other := value(e.Y, env)
				return a + b, ok && other
			}
			return "", false
		}
		visit = func(node ast.Node, env map[string]string) {
			ast.Inspect(node, func(n ast.Node) bool {
				if loop, ok := n.(*ast.RangeStmt); ok {
					literal, ok := loop.X.(*ast.CompositeLit)
					variable, isVariable := loop.Value.(*ast.Ident)
					if ok && isVariable {
						for _, element := range literal.Elts {
							text, ok := value(element, env)
							if !ok {
								continue
							}
							next := map[string]string{}
							for k, v := range env {
								next[k] = v
							}
							next[variable.Name] = text
							visit(loop.Body, next)
						}
						return false
					}
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "HandleFunc" {
					return true
				}
				pattern, ok := value(call.Args[0], env)
				if !ok {
					t.Errorf("cannot resolve route in %s", file.Name())
					return true
				}
				if pattern == "/" {
					return true
				} // JSON fallback, not a public operation.
				registered[pattern] = true
				return true
			})
		}
		visit(tree, nil)
	}
	for route := range registered {
		if !documented[route] {
			t.Errorf("missing OpenAPI operation: %s", route)
		}
	}
	for route := range documented {
		if !registered[route] {
			t.Errorf("documented operation has no handler: %s", route)
		}
	}
}

func TestOpenAPIDocumentIsPublicAndMatchesEmbeddedContract(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewRouter(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != string(openAPIDocument) {
		t.Fatalf("contract response mismatch: status %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("contract must be JSON")
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("contract must not be cached")
	}
}
