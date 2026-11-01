package i18n

import (
	"reflect"
	"testing"
)

func testBundle(t *testing.T) *Bundle {
	t.Helper()

	b, err := NewBundle(map[string][]byte{
		"en.yaml": []byte(`
language:
  name: English
nav:
  results: Results
greeting: "Hello {name}, you are {age}"
laps:
  one: "{count} lap"
  other: "{count} laps"
only_en: English only
js:
  confirm: Are you sure?
  cancel: Cancel
`),
		"de.yaml": []byte(`
language:
  name: Deutsch
nav:
  results: Ergebnisse
greeting: "Hallo {name}, du bist {age}"
laps:
  one: "{count} Runde"
  other: "{count} Runden"
js:
  confirm: Bist du sicher?
`),
	})

	if err != nil {
		t.Fatal(err)
	}

	return b
}

func TestBundleRequiresEnglish(t *testing.T) {
	if _, err := NewBundle(map[string][]byte{"de.yaml": []byte("a: b")}); err == nil {
		t.Fatal("expected an error without an English catalog")
	}
}

func TestCatalogsInSeveralFiles(t *testing.T) {
	b, err := NewBundle(map[string][]byte{
		"en.yaml":         []byte("a: A"),
		"results.en.yaml": []byte("results: {title: Results}"),
		"results.de.yaml": []byte("results: {title: Ergebnisse}"),
	})

	if err != nil {
		t.Fatal(err)
	}

	if got := b.Translator("en").T("a") + "/" + b.Translator("en").T("results.title") + "/" + b.Translator("de").T("results.title"); got != "A/Results/Ergebnisse" {
		t.Fatalf("got %q", got)
	}

	if _, err := NewBundle(map[string][]byte{"en.yaml": []byte("a: {b: x}"), "more.en.yaml": []byte("a: {b: y}")}); err == nil {
		t.Fatal("expected an error for a key in two files")
	}
}

func TestLanguages(t *testing.T) {
	got := testBundle(t).Languages()
	want := []Language{{Code: "en", Name: "English"}, {Code: "de", Name: "Deutsch"}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestTranslate(t *testing.T) {
	b := testBundle(t)
	de := b.Translator("de")

	cases := []struct{ got, want string }{
		{de.T("nav.results"), "Ergebnisse"},
		{de.T("greeting", "name", "Max", "age", 7), "Hallo Max, du bist 7"},
		{de.T("only_en"), "English only"},
		{de.T("does.not.exist"), "does.not.exist"},
		{de.N("laps", 1), "1 Runde"},
		{de.N("laps", 3), "3 Runden"},
		{b.Translator("en").N("laps", 0), "0 laps"},
		{b.Translator("fr").T("nav.results"), "Results"},
	}

	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: got %q, want %q", i, c.got, c.want)
		}
	}
}

func TestMessages(t *testing.T) {
	got := testBundle(t).Translator("de").Messages("js.")
	want := map[string]string{"confirm": "Bist du sicher?", "cancel": "Cancel"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMatch(t *testing.T) {
	b := testBundle(t)

	cases := map[string]string{
		"":                               "en",
		"de-DE,de;q=0.9,en-US;q=0.8":     "de",
		"de-AT":                          "de",
		"en-GB,en;q=0.9":                 "en",
		"fr-FR,fr;q=0.9":                 "en",
		"fr-FR,fr;q=0.9,de;q=0.8":        "de",
		"en-US,en;q=0.9,de-DE;q=0.8":     "en",
		"this is not a language header!": "en",
	}

	for header, want := range cases {
		if got := b.Match(header); got != want {
			t.Errorf("Match(%q) = %q, want %q", header, got, want)
		}
	}
}
