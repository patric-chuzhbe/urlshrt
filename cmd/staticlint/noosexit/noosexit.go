package noosexit

import (
	"go/ast"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer is a static analysis tool that reports the use of os.Exit()
// inside the main.main function. This is useful for detecting and avoiding
// abrupt termination of programs, which can interfere with defers, cleanup,
// and testability.
var Analyzer = &analysis.Analyzer{
	Name: "noosexit",
	Doc:  "prohibits direct use of os.Exit in main.main",
	Run:  run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	for _, file := range pass.Files {
		if pass.Pkg.Name() != "main" {
			continue
		}

		// Exclude go-build cache files
		filename := pass.Fset.File(file.Pos()).Name()
		if isGoBuildCacheFile(filename) {
			continue
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "main" || fn.Recv != nil {
				continue
			}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}

				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Exit" {
					return true
				}

				ident, ok := sel.X.(*ast.Ident)
				if ok && ident.Name == "os" {
					pass.Reportf(call.Pos(), "avoid using os.Exit in main.main")
				}

				return true
			})
		}
	}
	return nil, nil
}

func isGoBuildCacheFile(path string) bool {
	path = filepath.ToSlash(path)
	return strings.Contains(path, "/go-build/") || strings.Contains(path, `\go-build\`)
}
