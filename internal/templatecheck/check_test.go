// Package templatecheck holds a test which type checks the page templates against the Go types of their data.
package templatecheck

import (
	"fmt"
	"go/types"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"text/template/parse"
)

var (
	root      string
	namedByNm = map[string][]*types.Named{}
	problems  = map[string]bool{}
	visited   = map[string]bool{}
	allTrees  = map[string]*parse.Tree{}
	treeFile  = map[string]string{}
	fileSrc   = map[string]string{}
)

var overrides = map[string]string{
	"pages/championships/import-custom.html":       "championshipCustomRaceImportTemplateVars",
	"pages/championships/import-weekend.html":      "championshipRaceWeekendImportTemplateVars",
	"pages/content/weather.html":                   "weatherListTemplateVars",
	"pages/quick-race.html":                        "RaceTemplateVars",
	"pages/server/logs.html":                       "BaseTemplateVars",
	"pages/accounts/login.html":                    "BaseTemplateVars",
	"pages/championships/import-championship.html": "BaseTemplateVars",
	"pages/race-weekend/import-raceweekend.html":   "BaseTemplateVars",
}

func report(file string, n parse.Node, format string, a ...interface{}) {
	loc := file
	if n != nil {
		s := fileSrc[file]
		pos := int(n.Position())
		line := 1 + strings.Count(s[:minInt(pos, len(s))], "\n")
		loc = fmt.Sprintf("%s:%d", file, line)
	}
	msg := fmt.Sprintf("%s: %s", loc, fmt.Sprintf(format, a...))
	problems[msg] = true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func check(repoRoot string) ([]string, error) {
	root = repoRoot
	namedByNm = map[string][]*types.Named{}
	problems = map[string]bool{}
	visited = map[string]bool{}
	allTrees = map[string]*parse.Tree{}
	treeFile = map[string]string{}
	fileSrc = map[string]string{}
	funcArity = map[string]arity{}

	if err := loadTypes(root); err != nil {
		return nil, err
	}

	views := filepath.Join(root, "cmd/server-manager/views")
	var files []string
	filepath.Walk(views, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".html") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)

	gotypeRe := regexp.MustCompile(`gotype:\s*(\S*)\.([A-Za-z0-9_]+)`)
	type entry struct {
		file string
		typ  string
		pkg  string
	}
	var entries []entry
	for _, f := range files {
		b, _ := ioutil.ReadFile(f)
		rel, _ := filepath.Rel(views, f)
		rel = filepath.ToSlash(rel)
		src := strings.Replace(string(b), "\r\n", "\n", -1)
		fileSrc[rel] = src
		trees := parseAll(rel, src)
		for name, t := range trees {
			allTrees[rel+"#"+name] = t
			treeFile[rel+"#"+name] = rel
			if strings.HasPrefix(rel, "partials/") || strings.HasPrefix(rel, "layout/") {
				allTrees[name] = t
				treeFile[name] = rel
			}
		}
		if m := gotypeRe.FindStringSubmatch(src); m != nil {
			entries = append(entries, entry{rel, m[2], m[1]})
		} else if ov, ok := overrides[rel]; ok {
			entries = append(entries, entry{rel, ov, ""})
		} else if strings.HasPrefix(rel, "pages/") {
			report(rel, nil, "no gotype comment")
		}
	}
	entries = append(entries, entry{"layout/base.html", "BaseTemplateVars", ""})

	for _, e := range entries {
		ts := namedByNm[e.typ]
		if len(ts) == 0 {
			report(e.file, nil, "gotype %s not found in HEAD", e.typ)
			continue
		}
		if e.pkg != "" && ts[0].Obj().Pkg().Path() != e.pkg {
			report(e.file, nil, "gotype comment names package %s, but %s is declared in %s", e.pkg, e.typ, ts[0].Obj().Pkg().Path())
		}

		var dot types.Type = types.NewPointer(ts[0])
		prefix := e.file + "#"
		for k, t := range allTrees {
			if strings.HasPrefix(k, prefix) && (k == prefix+e.file || k == prefix+"content" || k == prefix+"title" || k == prefix+"partial") {
				a := &analyzer{file: e.file}
				a.walkTree(t, dot)
			}
		}
	}
	var out []string
	for p := range problems {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

var funcRe = regexp.MustCompile(`function "([^"]+)" not defined`)

func parseAll(name, src string) map[string]*parse.Tree {
	funcs := map[string]bool{}
	for i := 0; i < 400; i++ {
		fm := map[string]interface{}{}
		for k := range funcs {
			fm[k] = func() {}
		}
		trees, err := parse.Parse(name, src, "{{", "}}", fm)
		if err == nil {
			return trees
		}
		m := funcRe.FindStringSubmatch(err.Error())
		if m == nil {
			fmt.Fprintf(os.Stderr, "parse error %s: %v\n", name, err)
			return nil
		}
		funcs[m[1]] = true
	}
	return nil
}

type analyzer struct {
	file string
	vars []map[string]types.Type
}

func (a *analyzer) walkTree(t *parse.Tree, dot types.Type) {
	if t == nil || t.Root == nil {
		return
	}
	a.vars = []map[string]types.Type{{"$": dot}}
	a.walk(t.Root, dot)
}

func (a *analyzer) setVar(n string, t types.Type) {
	a.vars[len(a.vars)-1][n] = t
}

func (a *analyzer) getVar(n string) (types.Type, bool) {
	for i := len(a.vars) - 1; i >= 0; i-- {
		if t, ok := a.vars[i][n]; ok {
			return t, true
		}
	}
	return nil, false
}

func (a *analyzer) push() { a.vars = append(a.vars, map[string]types.Type{}) }
func (a *analyzer) pop()  { a.vars = a.vars[:len(a.vars)-1] }

func (a *analyzer) walk(n parse.Node, dot types.Type) {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			a.walk(c, dot)
		}
	case *parse.ActionNode:
		a.evalPipe(n.Pipe, dot)
	case *parse.IfNode:
		a.push()
		a.evalPipe(n.Pipe, dot)
		a.walk(n.List, dot)
		a.pop()
		if n.ElseList != nil {
			a.walk(n.ElseList, dot)
		}
	case *parse.WithNode:
		a.push()
		t := a.evalPipe(n.Pipe, dot)
		a.walk(n.List, t)
		a.pop()
		if n.ElseList != nil {
			a.walk(n.ElseList, dot)
		}
	case *parse.RangeNode:
		a.push()
		t := a.evalPipe(n.Pipe, dot)
		var elem, key types.Type
		if t != nil {
			elem, key = rangeTypes(t)
		}
		if len(n.Pipe.Decl) == 2 {
			a.setVar(n.Pipe.Decl[0].Ident[0], key)
			a.setVar(n.Pipe.Decl[1].Ident[0], elem)
		} else if len(n.Pipe.Decl) == 1 {
			a.setVar(n.Pipe.Decl[0].Ident[0], elem)
		}
		a.walk(n.List, elem)
		a.pop()
		if n.ElseList != nil {
			a.walk(n.ElseList, dot)
		}
	case *parse.TemplateNode:
		var arg types.Type
		if n.Pipe != nil {
			arg = a.evalPipe(n.Pipe, dot)
		}
		if arg != nil {
			key := n.Name + "|" + arg.String()
			if !visited[key] {
				visited[key] = true
				if t, ok := allTrees[a.file+"#"+n.Name]; ok {
					b := &analyzer{file: a.file}
					b.walkTree(t, arg)
				} else if t, ok := allTrees[n.Name]; ok {
					b := &analyzer{file: treeFile[n.Name]}
					b.walkTree(t, arg)
				}
			}
		}
	}
}

func rangeTypes(t types.Type) (elem, key types.Type) {
	u := deref(t).Underlying()
	switch u := u.(type) {
	case *types.Slice:
		return u.Elem(), types.Typ[types.Int]
	case *types.Array:
		return u.Elem(), types.Typ[types.Int]
	case *types.Map:
		return u.Elem(), u.Key()
	}
	return nil, nil
}

func deref(t types.Type) types.Type {
	if p, ok := t.(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

func (a *analyzer) evalPipe(p *parse.PipeNode, dot types.Type) types.Type {
	if p == nil {
		return nil
	}
	var t types.Type
	for i, c := range p.Cmds {
		t = a.evalCmd(c, dot, i > 0)
		if i > 0 {
			t = nil
		}
	}
	if len(p.Decl) == 1 {
		a.setVar(p.Decl[0].Ident[0], t)
	}
	return t
}

func (a *analyzer) evalCmd(c *parse.CommandNode, dot types.Type, piped bool) types.Type {
	if len(c.Args) == 0 {
		return nil
	}
	nargs := len(c.Args) - 1
	for _, arg := range c.Args[1:] {
		a.evalArgType(arg, dot)
	}
	switch h := c.Args[0].(type) {
	case *parse.FieldNode:
		return a.chain(dot, h.Ident, nargs, h)
	case *parse.VariableNode:
		vt, ok := a.getVar(h.Ident[0])
		if !ok {
			return nil
		}
		if len(h.Ident) == 1 {
			return vt
		}
		return a.chain(vt, h.Ident[1:], nargs, h)
	case *parse.DotNode:
		return dot
	case *parse.PipeNode:
		return a.evalPipe(h, dot)
	case *parse.IdentifierNode:
		if h.Ident == "index" && len(c.Args) >= 2 {
			t := a.evalArgType(c.Args[1], dot)
			if t != nil {
				switch u := deref(t).Underlying().(type) {
				case *types.Slice:
					return u.Elem()
				case *types.Map:
					return u.Elem()
				case *types.Array:
					return u.Elem()
				}
			}
		}
		if ar, ok := funcArity[h.Ident]; ok {
			n := len(c.Args) - 1
			if piped {
				n++
			}
			if ar.variadic {
				if n < ar.params-1 {
					report(a.file, h, "func %s needs at least %d args, got %d", h.Ident, ar.params-1, n)
				}
			} else if n != ar.params {
				report(a.file, h, "func %s wants %d args, got %d", h.Ident, ar.params, n)
			}
		}
		return nil
	}
	return nil
}

func (a *analyzer) evalArgType(arg parse.Node, dot types.Type) types.Type {
	switch h := arg.(type) {
	case *parse.FieldNode:
		return a.chain(dot, h.Ident, 0, h)
	case *parse.VariableNode:
		vt, ok := a.getVar(h.Ident[0])
		if !ok {
			return nil
		}
		if len(h.Ident) == 1 {
			return vt
		}
		return a.chain(vt, h.Ident[1:], 0, h)
	case *parse.DotNode:
		return dot
	case *parse.PipeNode:
		return a.evalPipe(h, dot)
	}
	return nil
}

// chain resolves a field chain starting at type t. nargs is the number of arguments given to the last element.
func (a *analyzer) chain(t types.Type, idents []string, nargs int, node parse.Node) types.Type {
	for i, id := range idents {
		if t == nil {
			return nil
		}
		last := i == len(idents)-1
		bt := t
		if it, ok := bt.Underlying().(*types.Interface); ok && it.NumMethods() == 0 {
			return nil
		}
		if m, ok := deref(bt).Underlying().(*types.Map); ok {
			t = m.Elem()
			continue
		}
		obj, _, _ := types.LookupFieldOrMethod(bt, true, nil, id)
		if obj == nil {
			obj, _, _ = types.LookupFieldOrMethod(types.NewPointer(deref(bt)), true, nil, id)
		}
		if obj == nil {
			report(a.file, node, "%s: no field or method %q on %s", chainStr(idents), id, bt.String())
			return nil
		}
		switch o := obj.(type) {
		case *types.Var:
			t = o.Type()
			if last && nargs > 0 {
				report(a.file, node, "%s: %q is a field but called with %d args", chainStr(idents), id, nargs)
			}
		case *types.Func:
			sig := o.Type().(*types.Signature)
			n := 0
			if last {
				n = nargs
			}
			np := sig.Params().Len()
			if sig.Variadic() {
				if n < np-1 {
					report(a.file, node, "%s: method %q needs at least %d args, got %d", chainStr(idents), id, np-1, n)
				}
			} else if n != np {
				report(a.file, node, "%s: method %q on %s wants %d args, got %d", chainStr(idents), id, bt.String(), np, n)
			}
			if sig.Results().Len() == 0 {
				return nil
			}
			t = sig.Results().At(0).Type()
		}
	}
	return t
}

func chainStr(ids []string) string { return "." + strings.Join(ids, ".") }

// TestTemplatesMatchGoTypes checks the field and method accesses of the page templates against the Go types of the
// template data (named in the gotype comment of each page, or in overrides), as well as the number of arguments of
// method calls and of the template functions. Templates are only checked at runtime otherwise, so a changed signature
// breaks pages (or text templates) that no test renders.
func TestTemplatesMatchGoTypes(t *testing.T) {
	if testing.Short() {
		t.Skip("type checks the whole module")
	}

	problems, err := check(filepath.Join("..", ".."))

	if err != nil {
		t.Fatal(err)
	}

	for _, p := range problems {
		t.Error(p)
	}
}
