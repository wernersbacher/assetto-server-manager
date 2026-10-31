package templatecheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const modulePath = "github.com/wernersbacher/ACPitwall"

// arity is the number of parameters of a template function.
type arity struct {
	params   int
	variadic bool
}

var funcArity = map[string]arity{}

// loadTypes type checks the packages of the module (from source) and collects their named types by name.
func loadTypes(repoRoot string) error {
	abs, err := filepath.Abs(repoRoot)

	if err != nil {
		return err
	}

	var dirs []string

	err = filepath.Walk(abs, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			name := info.Name()

			if name == "node_modules" || name == "typescript" || name == "static" || name == "views" || (strings.HasPrefix(name, ".") && p != abs) {
				return filepath.SkipDir
			}

			if p != abs && !strings.HasPrefix(filepath.ToSlash(p), filepath.ToSlash(filepath.Join(abs, "internal"))) {
				return filepath.SkipDir
			}

			dirs = append(dirs, p)
		}

		return nil
	})

	if err != nil {
		return err
	}

	sort.Strings(dirs)

	imp, err := newModuleImporter(abs)

	if err != nil {
		return err
	}

	for _, dir := range dirs {
		rel, _ := filepath.Rel(abs, dir)
		path := modulePath

		if rel != "." {
			path += "/" + filepath.ToSlash(rel)
		}

		pkg, err := imp.ImportFrom(path, abs, 0)

		if err != nil || pkg == nil {
			// directories without go files
			continue
		}

		for _, name := range pkg.Scope().Names() {
			if tn, ok := pkg.Scope().Lookup(name).(*types.TypeName); ok {
				if n, ok := tn.Type().(*types.Named); ok {
					namedByNm[name] = append(namedByNm[name], n)
				}
			}
		}

		collectFuncArity(dir, abs)
	}

	if len(imp.failures) > 0 {
		return fmt.Errorf("type checking the module failed: %s", strings.Join(imp.failures, "; "))
	}

	return nil
}

// collectFuncArity finds the template functions registered in the FuncMap literals and funcs["name"] = fn assignments
// of a package directory, and records how many parameters they take.
func collectFuncArity(dir, abs string) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)

	if err != nil {
		return
	}

	for _, pkg := range pkgs {
		decls := map[string]*ast.FuncDecl{}

		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
					decls[fd.Name.Name] = fd
				}
			}
		}

		for _, f := range pkg.Files {
			imports := map[string]string{}

			for _, im := range f.Imports {
				p, _ := strconv.Unquote(im.Path.Value)
				name := filepath.Base(p)

				if im.Name != nil {
					name = im.Name.Name
				}

				imports[name] = p
			}

			resolve := func(e ast.Expr) (arity, bool) {
				switch e := e.(type) {
				case *ast.FuncLit:
					return fieldListArity(e.Type)
				case *ast.Ident:
					if fd, ok := decls[e.Name]; ok {
						return fieldListArity(fd.Type)
					}
				case *ast.SelectorExpr:
					if x, ok := e.X.(*ast.Ident); ok {
						if p, ok := imports[x.Name]; ok && strings.HasPrefix(p, modulePath) {
							return lookupFuncArity(filepath.Join(abs, strings.TrimPrefix(p, modulePath)), e.Sel.Name)
						}
					}
				}

				return arity{}, false
			}

			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CompositeLit:
					for _, el := range n.Elts {
						kv, ok := el.(*ast.KeyValueExpr)

						if !ok {
							continue
						}

						bl, ok := kv.Key.(*ast.BasicLit)

						if !ok || bl.Kind != token.STRING {
							continue
						}

						if a, ok := resolve(kv.Value); ok {
							name, _ := strconv.Unquote(bl.Value)
							funcArity[name] = a
						}
					}
				case *ast.AssignStmt:
					if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
						if ie, ok := n.Lhs[0].(*ast.IndexExpr); ok {
							if bl, ok := ie.Index.(*ast.BasicLit); ok && bl.Kind == token.STRING {
								if a, ok := resolve(n.Rhs[0]); ok {
									name, _ := strconv.Unquote(bl.Value)
									funcArity[name] = a
								}
							}
						}
					}
				}

				return true
			})
		}
	}
}

func lookupFuncArity(dir, name string) (arity, bool) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)

	if err != nil {
		return arity{}, false
	}

	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
					return fieldListArity(fd.Type)
				}
			}
		}
	}

	return arity{}, false
}

func fieldListArity(ft *ast.FuncType) (arity, bool) {
	a := arity{}

	if ft.Params == nil {
		return a, true
	}

	for _, f := range ft.Params.List {
		if _, ok := f.Type.(*ast.Ellipsis); ok {
			a.variadic = true
		}

		if len(f.Names) == 0 {
			a.params++
		} else {
			a.params += len(f.Names)
		}
	}

	return a, true
}
