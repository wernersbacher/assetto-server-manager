package portcheck

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
)

// The templates translate the ports by their key (t .Key), which the catalog test can't see: every key has to be in
// the English catalog with the English text of the code.
func TestKeysAreInTheCatalog(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "cmd", "acpitwall", "views", "locales", "*.yaml"))

	if err != nil {
		t.Fatal(err)
	}

	locales := make(map[string][]byte)

	for _, file := range files {
		if locales[filepath.Base(file)], err = ioutil.ReadFile(file); err != nil {
			t.Fatal(err)
		}
	}

	bundle, err := i18n.NewBundle(locales)

	if err != nil {
		t.Fatal(err)
	}

	en := bundle.Translator(i18n.Default)

	for key, what := range Keys {
		if msg, ok := en.Lookup(key); !ok || msg != what {
			t.Errorf("%s: the English catalog has %q, the code %q", key, msg, what)
		}
	}
}
