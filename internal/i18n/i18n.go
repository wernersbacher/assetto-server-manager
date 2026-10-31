// Package i18n holds the message catalogs of the user interface and picks the language of a request.
//
// A catalog is a YAML file per language (named <code>.yaml, e.g. de.yaml). Nested keys are flattened to dotted keys
// ("nav.results"). English ("en") is the reference catalog: every key has to exist there, other catalogs may miss keys,
// which then fall back to English (and to the key itself if English misses it too). The key "language.name" holds the
// name of the language in that language, it is shown in the language selection. Adding a language only takes a new
// catalog file.
//
// Messages may contain placeholders like {name}, which are filled from name/value pairs. Plurals are sub keys named
// after the plural form of the language ("one", "other"), see N.
package i18n

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/language"
	"gopkg.in/yaml.v2"
)

// Default is the language which is used if nothing else matches, and the reference catalog.
const Default = "en"

// nameKey is the key of the language's own name.
const nameKey = "language.name"

// Language is a language with a catalog.
type Language struct {
	// Code is the (BCP 47) code of the language, e.g. "de".
	Code string
	// Name is the name of the language in the language itself, e.g. "Deutsch".
	Name string
}

// Bundle holds the catalogs of all languages.
type Bundle struct {
	languages []Language
	catalogs  map[string]map[string]string
	matcher   language.Matcher
	tags      []language.Tag
}

// NewBundle parses the catalogs, given as the contents of the YAML files by language code. The English catalog is
// required.
func NewBundle(files map[string][]byte) (*Bundle, error) {
	b := &Bundle{catalogs: make(map[string]map[string]string)}

	for code, data := range files {
		tag, err := language.Parse(code)

		if err != nil {
			return nil, fmt.Errorf("i18n: invalid language code %q: %w", code, err)
		}

		code = tag.String()

		var raw map[interface{}]interface{}

		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("i18n: catalog %s: %w", code, err)
		}

		messages := make(map[string]string)

		if err := flatten("", raw, messages); err != nil {
			return nil, fmt.Errorf("i18n: catalog %s: %w", code, err)
		}

		b.catalogs[code] = messages
	}

	if _, ok := b.catalogs[Default]; !ok {
		return nil, fmt.Errorf("i18n: the %s catalog is missing", Default)
	}

	codes := make([]string, 0, len(b.catalogs))

	for code := range b.catalogs {
		if code != Default {
			codes = append(codes, code)
		}
	}

	sort.Strings(codes)
	codes = append([]string{Default}, codes...)

	for _, code := range codes {
		name := b.catalogs[code][nameKey]

		if name == "" {
			name = code
		}

		b.languages = append(b.languages, Language{Code: code, Name: name})
		b.tags = append(b.tags, language.MustParse(code))
	}

	b.matcher = language.NewMatcher(b.tags)

	return b, nil
}

func flatten(prefix string, in map[interface{}]interface{}, out map[string]string) error {
	for k, v := range in {
		key, ok := k.(string)

		if !ok {
			// YAML reads unquoted yes/no/on/off/numbers as other types
			return fmt.Errorf("key %v below %q is not a string, quote it", k, prefix)
		}

		if prefix != "" {
			key = prefix + "." + key
		}

		switch v := v.(type) {
		case map[interface{}]interface{}:
			if err := flatten(key, v, out); err != nil {
				return err
			}
		case string:
			out[key] = v
		case nil:
			// an empty entry, e.g. a key which is not translated yet
		case bool, int, float64:
			out[key] = fmt.Sprint(v)
		default:
			return fmt.Errorf("key %s: unsupported value of type %T", key, v)
		}
	}

	return nil
}

// Languages returns the languages with a catalog, English first, the others sorted by code.
func (b *Bundle) Languages() []Language {
	return b.languages
}

// Supports tells if there is a catalog for the language code.
func (b *Bundle) Supports(code string) bool {
	_, ok := b.catalogs[code]

	return ok
}

// Keys returns all keys of the catalog of the language, sorted.
func (b *Bundle) Keys(code string) []string {
	keys := make([]string, 0, len(b.catalogs[code]))

	for key := range b.catalogs[code] {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// Match returns the supported language which fits an Accept-Language header best, the default language if none does.
func (b *Bundle) Match(acceptLanguage string) string {
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)

	if err != nil || len(tags) == 0 {
		return Default
	}

	_, index, confidence := b.matcher.Match(tags...)

	if confidence == language.No {
		return Default
	}

	return b.languages[index].Code
}

// Translator returns the translator of the language, the default language's if there is no catalog for it.
func (b *Bundle) Translator(code string) *Translator {
	if !b.Supports(code) {
		code = Default
	}

	return &Translator{
		lang:     code,
		messages: b.catalogs[code],
		fallback: b.catalogs[Default],
		plural:   pluralRule(code),
	}
}

// Translator translates the messages of one language.
type Translator struct {
	lang     string
	messages map[string]string
	fallback map[string]string
	plural   func(n int) string
}

// Lang returns the code of the language.
func (t *Translator) Lang() string {
	return t.lang
}

func (t *Translator) lookup(key string) (string, bool) {
	if msg, ok := t.messages[key]; ok {
		return msg, true
	}

	msg, ok := t.fallback[key]

	return msg, ok
}

// T returns the message of the key with its placeholders filled from args, which are name/value pairs:
// T("results.count", "count", 3). Missing keys give the key itself, so that they stand out.
func (t *Translator) T(key string, args ...interface{}) string {
	msg, ok := t.lookup(key)

	if !ok {
		return key
	}

	return fill(msg, args)
}

// N returns the plural form of the key which fits n, with the placeholder {count} set to n: the catalog holds the forms
// as sub keys (key.one, key.other). Forms the language does not have fall back to "other".
func (t *Translator) N(key string, n int, args ...interface{}) string {
	msg, ok := t.lookup(key + "." + t.plural(n))

	if !ok {
		msg, ok = t.lookup(key + ".other")
	}

	if !ok {
		return key
	}

	return fill(msg, append([]interface{}{"count", n}, args...))
}

// Messages returns the messages whose keys start with prefix (e.g. "js."), with the prefix removed and the English
// messages for keys the language misses.
func (t *Translator) Messages(prefix string) map[string]string {
	out := make(map[string]string)

	for _, catalog := range []map[string]string{t.fallback, t.messages} {
		for key, msg := range catalog {
			if strings.HasPrefix(key, prefix) {
				out[strings.TrimPrefix(key, prefix)] = msg
			}
		}
	}

	return out
}

// fill replaces the placeholders {name} of msg with the values of the name/value pairs in args.
func fill(msg string, args []interface{}) string {
	if len(args) < 2 || !strings.Contains(msg, "{") {
		return msg
	}

	pairs := make([]string, 0, len(args))

	for i := 0; i+1 < len(args); i += 2 {
		pairs = append(pairs, "{"+fmt.Sprint(args[i])+"}", format(args[i+1]))
	}

	return strings.NewReplacer(pairs...).Replace(msg)
}

func format(v interface{}) string {
	switch v := v.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

// pluralRule returns the plural form of a count in the language (CLDR names). English and German only know "one" and
// "other"; languages with other rules get theirs here when they are added.
func pluralRule(code string) func(n int) string {
	switch code {
	default:
		return func(n int) string {
			if n == 1 {
				return "one"
			}

			return "other"
		}
	}
}
