package servermanager

import (
	"bytes"
	"io/ioutil"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// formCatalog is the English catalog of the forms which are generated from struct tags (smweb.EncodeFormData). It is
// generated from the tags, so that it can't drift from them: UPDATE_FORM_CATALOG=1 go test -run TestFormCatalog .
var formCatalog = filepath.Join("cmd", "acpitwall", "views", "locales", "form.en.yaml")

// formData are the values of all forms which are rendered with smweb.EncodeFormData.
func formData() []interface{} {
	serverConfig := serverconfig.ConfigIniDefault()

	return []interface{}{
		&serverConfig.GlobalServerConfig,
		smplugins.DefaultStrackerIni(""),
		smplugins.DefaultKissMyRankConfig(),
		smplugins.DefaultRealPenaltyConfig(),
	}
}

func TestFormCatalog(t *testing.T) {
	texts := make(map[string]string)

	for _, data := range formData() {
		formTexts, err := smweb.FormTexts(data)

		if err != nil {
			t.Fatal(err)
		}

		for key, text := range formTexts {
			texts[key] = text
		}
	}

	want, err := encodeNestedYAML(texts)

	if err != nil {
		t.Fatal(err)
	}

	header := "# Generated from the struct tags of the forms (see form_catalog_test.go), do not edit:\n" +
		"# UPDATE_FORM_CATALOG=1 go test -run TestFormCatalog .\n"

	want = append([]byte(header), want...)

	if os.Getenv("UPDATE_FORM_CATALOG") != "" {
		if err := ioutil.WriteFile(formCatalog, want, 0644); err != nil {
			t.Fatal(err)
		}

		return
	}

	got, err := ioutil.ReadFile(formCatalog)

	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(bytes.Replace(got, []byte("\r\n"), []byte("\n"), -1), want) {
		t.Fatalf("%s is out of date, run: UPDATE_FORM_CATALOG=1 go test -run TestFormCatalog .", formCatalog)
	}
}

// encodeNestedYAML writes dotted keys as nested, sorted YAML maps.
func encodeNestedYAML(flat map[string]string) ([]byte, error) {
	type node struct {
		value    *string
		children map[string]*node
	}

	root := &node{children: map[string]*node{}}

	for key, value := range flat {
		n := root

		for _, part := range strings.Split(key, ".") {
			child, ok := n.children[part]

			if !ok {
				child = &node{children: map[string]*node{}}
				n.children[part] = child
			}

			n = child
		}

		v := value
		n.value = &v
	}

	var convert func(n *node) yaml.MapSlice

	convert = func(n *node) yaml.MapSlice {
		keys := make([]string, 0, len(n.children))

		for key := range n.children {
			keys = append(keys, key)
		}

		sort.Strings(keys)

		out := yaml.MapSlice{}

		for _, key := range keys {
			child := n.children[key]

			if child.value != nil {
				out = append(out, yaml.MapItem{Key: key, Value: *child.value})
			} else {
				out = append(out, yaml.MapItem{Key: key, Value: convert(child)})
			}
		}

		return out
	}

	return yaml.Marshal(convert(root))
}

func TestFormsAreTranslated(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("cmd", "acpitwall", "views", "locales", "*.yaml"))

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

	bundle, err := i18n.NewBundle(locales)

	if err != nil {
		t.Fatal(err)
	}

	serverConfig := serverconfig.ConfigIniDefault()

	for lang, want := range map[string]string{"en": "Admin Password", "de": "Admin-Passwort"} {
		r := httptest.NewRequest("GET", "/server-options", nil)
		r.Header.Set("Accept-Language", lang)

		form, err := smweb.EncodeFormData(&serverConfig.GlobalServerConfig, smweb.WithBundle(r, bundle))

		if err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(string(form), want) {
			t.Errorf("%s: the form has no label %q", lang, want)
		}
	}
}
