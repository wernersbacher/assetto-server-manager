package web

import (
	"bytes"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

type memoryLoader struct {
	page    string
	locales map[string][]byte
}

func (m memoryLoader) Init() error { return nil }

func (m memoryLoader) Templates(funcs template.FuncMap) (map[string]*template.Template, error) {
	t, err := template.New("page").Funcs(funcs).Parse(m.page)

	if err != nil {
		return nil, err
	}

	return map[string]*template.Template{"page.html": t}, nil
}

func (m memoryLoader) Locales() (map[string][]byte, error) { return m.locales, nil }

func TestRendererTranslatesPerRequest(t *testing.T) {
	tr, err := NewRenderer(RendererOptions{
		Loader: memoryLoader{
			page: `{{ define "base" }}{{ lang }}: {{ t "hello" "name" . }} {{ tn "laps" 2 }} {{ tHTML "link" "x" . }} {{ yn true }}{{ end }}`,
			locales: map[string][]byte{
				"en.yaml": []byte(`{hello: "Hello {name}", laps: {one: "{count} lap", other: "{count} laps"}, link: "<b>{x}</b>", common: {"yes": "Yes"}}`),
				"de.yaml": []byte(`{hello: "Hallo {name}", laps: {one: "{count} Runde", other: "{count} Runden"}, link: "<i>{x}</i>", common: {"yes": "Ja"}}`),
			},
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		accept, cookie, want string
	}{
		{"", "", "en: Hello &lt;Max&gt; 2 laps <b>&lt;Max&gt;</b> Yes"},
		{"de-DE,de;q=0.9", "", "de: Hallo &lt;Max&gt; 2 Runden <i>&lt;Max&gt;</i> Ja"},
		{"de-DE,de;q=0.9", "en", "en: Hello &lt;Max&gt; 2 laps <b>&lt;Max&gt;</b> Yes"},
		{"", "de", "de: Hallo &lt;Max&gt; 2 Runden <i>&lt;Max&gt;</i> Ja"},
		{"de", "xx", "de: Hallo &lt;Max&gt; 2 Runden <i>&lt;Max&gt;</i> Ja"},
	}

	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", c.accept)

		if c.cookie != "" {
			r.Header.Set("Cookie", LanguageCookie+"="+c.cookie)
		}

		page, ok := tr.template(r, "page.html")

		if !ok {
			t.Fatal("page not found")
		}

		buf := new(bytes.Buffer)

		if err := page.proto.ExecuteTemplate(buf, "base", "<Max>"); err != nil {
			t.Fatal(err)
		}

		if buf.String() != c.want {
			t.Errorf("accept %q, cookie %q: got %q, want %q", c.accept, c.cookie, buf.String(), c.want)
		}
	}

	if got := len(tr.Bundle().Languages()); got != 2 {
		t.Errorf("got %d languages, want 2", got)
	}
}

func TestTranslateForRequest(t *testing.T) {
	tr, err := NewRenderer(RendererOptions{
		Loader: memoryLoader{
			page: `{{ define "base" }}{{ end }}`,
			locales: map[string][]byte{
				"en.yaml": []byte(`{flash: {saved: "{name} saved"}}`),
				"de.yaml": []byte(`{flash: {saved: "{name} gespeichert"}}`),
			},
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("GET", "/", nil)

	if got := T(r, "flash.saved", "name", "X"); got != "flash.saved" {
		t.Errorf("without catalogs: got %q", got)
	}

	r.Header.Set("Accept-Language", "de")

	var got string

	tr.TranslationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = T(r, "flash.saved", "name", "X")
	})).ServeHTTP(httptest.NewRecorder(), r)

	if got != "X gespeichert" {
		t.Errorf("got %q", got)
	}
}

func TestCanSeeIsBoundPerRequestInEveryLanguage(t *testing.T) {
	tr, err := NewRenderer(RendererOptions{
		Loader: memoryLoader{
			page: `{{ define "link" }}{{ if canSee "results" }}results{{ else }}-{{ end }}{{ end }}` +
				`{{ define "base" }}{{ if canSee "teams" }}teams{{ else }}-{{ end }},{{ template "link" . }},{{ if canSee "bogus" }}bogus{{ else }}-{{ end }}{{ end }}`,
			locales: map[string][]byte{
				"en.yaml": []byte(`{hello: "Hello"}`),
				"de.yaml": []byte(`{hello: "Hallo"}`),
			},
		},
	})

	if err != nil {
		t.Fatal(err)
	}

	render := func(lang string, visibility smmodel.Visibility, account *smmodel.Account) string {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Cookie", LanguageCookie+"="+lang)
		r = WithScope(r, &Scope{ServerID: "a", Visibility: visibility})

		if account != nil {
			r = WithAccount(r, account)
		}

		page, ok := tr.template(r, "page.html")

		if !ok {
			t.Fatal("page not found")
		}

		tmpl, err := page.get()

		if err != nil {
			t.Fatal(err)
		}

		defer page.put(tmpl)

		buf := new(bytes.Buffer)

		if err := tmpl.Funcs(requestFuncs(r)).ExecuteTemplate(buf, "base", nil); err != nil {
			t.Fatal(err)
		}

		return buf.String()
	}

	reader := &smmodel.Account{Name: "reader", Groups: map[smmodel.ServerID]smmodel.Group{"a": smmodel.GroupRead}}
	writer := &smmodel.Account{Name: "writer", Groups: map[smmodel.ServerID]smmodel.Group{"a": smmodel.GroupWrite}}

	cases := []struct {
		name       string
		visibility smmodel.Visibility
		account    *smmodel.Account
		want       string
	}{
		{"defaults, anonymous: teams are for accounts", nil, nil, "-,results,-"},
		{"defaults, reader", nil, reader, "teams,results,-"},
		{"results for accounts, anonymous", smmodel.Visibility{smmodel.AreaResults: smmodel.LevelLoggedIn}, nil, "-,-,-"},
		{"results for accounts, reader", smmodel.Visibility{smmodel.AreaResults: smmodel.LevelLoggedIn}, reader, "teams,results,-"},
		{"results for writers, reader", smmodel.Visibility{smmodel.AreaResults: smmodel.LevelWrite}, reader, "teams,-,-"},
		{"results for writers, writer", smmodel.Visibility{smmodel.AreaResults: smmodel.LevelWrite}, writer, "teams,results,-"},
		{"teams public, anonymous", smmodel.Visibility{smmodel.AreaTeams: smmodel.LevelPublic}, nil, "teams,results,-"},
	}

	for _, lang := range []string{"en", "de"} {
		for _, c := range cases {
			if got := render(lang, c.visibility, c.account); got != c.want {
				t.Errorf("%s (%s): got %q, want %q", c.name, lang, got, c.want)
			}
		}
	}
}
