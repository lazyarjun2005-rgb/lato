package index

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// extractGo parses the Go source held in f.Body and records deterministic
// symbol information (packages, imports, functions, methods, structs,
// interfaces, and named types) onto f. It never fails: malformed or
// unparseable files simply yield no symbols.
func extractGo(f *File) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, f.Path, f.Body, parser.SkipObjectResolution)
	if err != nil {
		return
	}

	if file.Name != nil && file.Name.Name != "" {
		f.Packages = appendIfMissing(f.Packages, file.Name.Name)
	}
	for _, imp := range file.Imports {
		if imp.Path != nil {
			f.Imports = appendIfMissing(f.Imports, imp.Path.Value)
		}
	}

	// Track the enclosing GenDecl so a ValueSpec knows whether it came
	// from a const or var declaration.
	var enclosing *ast.GenDecl

	ast.Inspect(file, func(n ast.Node) bool {
		switch d := n.(type) {
		case *ast.GenDecl:
			enclosing = d
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name != nil {
						f.Symbols = append(f.Symbols, Symbol{
							Kind: kindOf(ts),
							Name: ts.Name.Name,
							Pkg:  file.Name.Name,
							Pos:  fset.Position(ts.Pos()).Line,
						})
					}
				}
			}
		case *ast.ValueSpec:
			kind := "variable"
			if enclosing != nil && enclosing.Tok == token.CONST {
				kind = "constant"
			}
			for _, name := range d.Names {
				if name == nil {
					continue
				}
				f.Symbols = append(f.Symbols, Symbol{
					Kind: kind,
					Name: name.Name,
					Pkg:  file.Name.Name,
					Pos:  fset.Position(name.Pos()).Line,
				})
			}
		case *ast.FuncDecl:
			if d.Name == nil {
				return true
			}
			kind := "function"
			if d.Recv != nil {
				kind = "method"
			}
			f.Symbols = append(f.Symbols, Symbol{
				Kind: kind,
				Name: d.Name.Name,
				Pkg:  file.Name.Name,
				Pos:  fset.Position(d.Pos()).Line,
			})
		}
		return true
	})
}

// kindOf maps a TypeSpec's underlying type to a symbol kind.
func kindOf(ts *ast.TypeSpec) string {
	switch ts.Type.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	default:
		return "type"
	}
}

// appendIfMissing appends s to list when it is not already present.
func appendIfMissing(list []string, s string) []string {
	for _, existing := range list {
		if existing == s {
			return list
		}
	}
	return append(list, s)
}
