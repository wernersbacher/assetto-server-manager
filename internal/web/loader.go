package web

import (
	"html/template"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/mattn/go-zglob"
)

type TemplateLoader interface {
	Init() error
	Templates(funcs template.FuncMap) (map[string]*template.Template, error)

	// Locales returns the message catalogs (locales/*.yaml) by file name, see i18n.CatalogCode.
	Locales() (map[string][]byte, error)
}

// LocaleFile returns the file name of a catalog path (locales/de.yaml -> de.yaml), false if it is no catalog.
func LocaleFile(path string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(path), "/")

	if len(parts) < 2 || parts[len(parts)-2] != "locales" || !strings.HasSuffix(parts[len(parts)-1], ".yaml") {
		return "", false
	}

	return parts[len(parts)-1], true
}

func NewFilesystemTemplateLoader(dir string) TemplateLoader {
	return &filesystemTemplateLoader{
		dir: dir,
	}
}

type filesystemTemplateLoader struct {
	dir string

	pages, partials []string
}

func (fs *filesystemTemplateLoader) Init() error {
	var err error

	fs.pages, err = zglob.Glob(filepath.Join(fs.dir, "pages", "**", "*.html"))

	if err != nil {
		return err
	}

	fs.partials, err = zglob.Glob(filepath.Join(fs.dir, "partials", "**", "*.html"))

	if err != nil {
		return err
	}

	return nil
}

func (fs *filesystemTemplateLoader) Templates(funcs template.FuncMap) (map[string]*template.Template, error) {
	templates := make(map[string]*template.Template)

	for _, page := range fs.pages {
		var templateList []string
		templateList = append(templateList, filepath.Join(fs.dir, "layout", "base.html"))
		templateList = append(templateList, fs.partials...)
		templateList = append(templateList, page)

		t, err := template.New(page).Funcs(funcs).ParseFiles(templateList...)

		if err != nil {
			return nil, err
		}

		templates[strings.TrimPrefix(filepath.ToSlash(page), filepath.ToSlash(fs.dir)+"/pages/")] = t
	}

	return templates, nil
}

func (fs *filesystemTemplateLoader) Locales() (map[string][]byte, error) {
	files, err := filepath.Glob(filepath.Join(fs.dir, "locales", "*.yaml"))

	if err != nil {
		return nil, err
	}

	locales := make(map[string][]byte)

	for _, file := range files {
		name, ok := LocaleFile(file)

		if !ok {
			continue
		}

		data, err := ioutil.ReadFile(file)

		if err != nil {
			return nil, err
		}

		locales[name] = data
	}

	return locales, nil
}