package serverenv_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/wernersbacher/assetto-server-manager/internal/serverenv/envtest"
)

func TestEnvResultsDirs(t *testing.T) {
	root, err := ioutil.TempDir("", "asm-test-env")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	own, other := filepath.Join(root, "own"), filepath.Join(root, "other")
	env := envtest.New(own)

	if loader := env.Results(); loader.Dir != filepath.Join(own, "results") || len(loader.ExtraDirs) != 0 {
		t.Errorf("expected only the own results dir, got %q and %v", loader.Dir, loader.ExtraDirs)
	}

	env.ResultsDirs = func() []string {
		return []string{filepath.Join(own, "results"), filepath.Join(other, "results")}
	}

	loader := env.Results()

	if loader.Dir != filepath.Join(own, "results") {
		t.Errorf("unexpected write dir %q", loader.Dir)
	}

	if len(loader.ExtraDirs) != 1 || loader.ExtraDirs[0] != filepath.Join(other, "results") {
		t.Errorf("unexpected extra dirs %v", loader.ExtraDirs)
	}
}
