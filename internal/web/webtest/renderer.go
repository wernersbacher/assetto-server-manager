package webtest

import (
	"html/template"
	"testing"

	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// Languages are minimal catalogs of English and German for handlers which check language codes.
var Languages = map[string][]byte{
	"en.yaml": []byte("language: {name: English}"),
	"de.yaml": []byte("language: {name: Deutsch}"),
}

// memoryLoader is a template loader without templates.
type memoryLoader struct {
	locales map[string][]byte
}

func (m memoryLoader) Init() error { return nil }

func (m memoryLoader) Templates(template.FuncMap) (map[string]*template.Template, error) {
	return map[string]*template.Template{}, nil
}

func (m memoryLoader) Locales() (map[string][]byte, error) { return m.locales, nil }

// Renderer returns a renderer without templates which knows the catalogs (Languages if nil).
func Renderer(t *testing.T, locales map[string][]byte) *smweb.Renderer {
	t.Helper()

	if locales == nil {
		locales = Languages
	}

	renderer, err := smweb.NewRenderer(smweb.RendererOptions{Loader: memoryLoader{locales: locales}})

	if err != nil {
		t.Fatal(err)
	}

	return renderer
}
