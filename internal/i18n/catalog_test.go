package i18n

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// complete lists the languages whose catalogs have to translate every key. Languages which are added later may be
// incomplete for a while, their missing keys fall back to English.
var complete = []string{"en", "de"}

const viewsDir = "../../cmd/acpitwall/views"

func loadCatalogs(t *testing.T) *Bundle {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(viewsDir, "locales", "*.yaml"))

	if err != nil {
		t.Fatal(err)
	}

	locales := make(map[string][]byte)

	for _, file := range files {
		data, err := ioutil.ReadFile(file)

		if err != nil {
			t.Fatal(err)
		}

		locales[filepath.Base(file)] = data
	}

	b, err := NewBundle(locales)

	if err != nil {
		t.Fatal(err)
	}

	return b
}

var placeholderRe = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

func placeholders(msg string) string {
	found := placeholderRe.FindAllString(msg, -1)
	sort.Strings(found)

	return strings.Join(found, ",")
}

// TestCatalogsMatchEnglish checks that the catalogs only hold keys English has, with the same placeholders, and that
// the complete languages translate every key.
func TestCatalogsMatchEnglish(t *testing.T) {
	b := loadCatalogs(t)
	en := b.catalogs[Default]

	for _, lang := range b.Languages() {
		catalog := b.catalogs[lang.Code]

		for key, msg := range catalog {
			ref, ok := en[key]

			if !ok {
				t.Errorf("%s: key %s is not in the English catalog", lang.Code, key)
				continue
			}

			if placeholders(msg) != placeholders(ref) {
				t.Errorf("%s: key %s has the placeholders %q, English has %q", lang.Code, key, placeholders(msg), placeholders(ref))
			}
		}
	}

	for _, code := range complete {
		if !b.Supports(code) {
			t.Errorf("there is no catalog for %s", code)
			continue
		}

		for key := range en {
			if _, ok := b.catalogs[code][key]; !ok {
				t.Errorf("%s: key %s is not translated", code, key)
			}
		}
	}
}

// keyUseRe finds the keys of the translation functions in templates ({{ t "key" }}, tn, tHTML) and in the TypeScript
// code (t("key"), tn("key", ...), whose keys live below js.).
var (
	templateKeyRe = regexp.MustCompile(`\b(?:t|tn|tHTML)\s+"([^"]+)"`)
	scriptKeyRe   = regexp.MustCompile(`\b(?:t|tn)\(\s*["']([^"']+)["']`)
)

// TestUsedKeysExist checks that every key used by a template or the TypeScript code is in the English catalog, with
// the plural forms for tn.
func TestUsedKeysExist(t *testing.T) {
	b := loadCatalogs(t)
	en := b.catalogs[Default]

	check := func(file, key string, plural bool) {
		if plural {
			if _, ok := en[key+".other"]; !ok {
				t.Errorf("%s: plural key %s has no %s.other in the English catalog", file, key, key)
			}

			return
		}

		if _, ok := en[key]; !ok {
			t.Errorf("%s: key %s is not in the English catalog", file, key)
		}
	}

	walk := func(dir, suffix string, re *regexp.Regexp, prefix string) {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, suffix) || strings.Contains(path, "node_modules") {
				return nil
			}

			data, err := ioutil.ReadFile(path)

			if err != nil {
				t.Fatal(err)
			}

			for _, m := range re.FindAllStringSubmatch(string(data), -1) {
				plural := strings.HasPrefix(strings.TrimSpace(m[0]), "tn")
				check(path, prefix+m[1], plural)
			}

			return nil
		})
	}

	walk(viewsDir, ".html", templateKeyRe, "")
	walk("../../cmd/acpitwall/typescript/src", ".ts", scriptKeyRe, "js.")
}
