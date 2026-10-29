package templatecheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type listedPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Export     string
}

// moduleImporter type checks the packages of the module from source (so that their unexported types are available)
// and reads everything else from the export data the go tool produced.
type moduleImporter struct {
	fset     *token.FileSet
	listed   map[string]*listedPackage
	external types.Importer
	checked  map[string]*types.Package
	failures []string
}

func newModuleImporter(repoRoot string) (*moduleImporter, error) {
	cmd := exec.Command("go", "list", "-export", "-deps", "-json", "./...")
	cmd.Dir = repoRoot

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()

	if err != nil {
		return nil, fmt.Errorf("go list: %v: %s", err, stderr.String())
	}

	mi := &moduleImporter{
		fset:    token.NewFileSet(),
		listed:  make(map[string]*listedPackage),
		checked: make(map[string]*types.Package),
	}

	dec := json.NewDecoder(bytes.NewReader(out))

	for dec.More() {
		var p listedPackage

		if err := dec.Decode(&p); err != nil {
			return nil, err
		}

		mi.listed[p.ImportPath] = &p
	}

	mi.external = importer.ForCompiler(mi.fset, "gc", func(path string) (io.ReadCloser, error) {
		p, ok := mi.listed[path]

		if !ok || p.Export == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}

		return os.Open(p.Export)
	})

	return mi, nil
}

func (mi *moduleImporter) Import(path string) (*types.Package, error) {
	return mi.ImportFrom(path, "", 0)
}

func (mi *moduleImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}

	if !strings.HasPrefix(path, modulePath) {
		return mi.external.Import(path)
	}

	if pkg, ok := mi.checked[path]; ok {
		return pkg, nil
	}

	lp, ok := mi.listed[path]

	if !ok {
		return nil, fmt.Errorf("package %s not listed", path)
	}

	var files []*ast.File

	for _, name := range lp.GoFiles {
		f, err := parser.ParseFile(mi.fset, filepath.Join(lp.Dir, name), nil, 0)

		if err != nil {
			return nil, err
		}

		files = append(files, f)
	}

	conf := types.Config{
		Importer: mi,
		Error:    func(err error) { mi.failures = append(mi.failures, err.Error()) },
	}

	pkg, _ := conf.Check(path, mi.fset, files, nil)
	mi.checked[path] = pkg

	return pkg, nil
}
