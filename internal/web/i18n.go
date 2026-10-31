package web

import (
	"fmt"
	"html/template"
	"net/http"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
)

// LanguageCookie remembers the language which was picked in the language selection.
const LanguageCookie = "lang"

// RequestLanguage returns the code of the language a request is answered in: the language picked in the language
// selection (cookie), else the best match of the browser's Accept-Language header, else the default language.
func RequestLanguage(r *http.Request, bundle *i18n.Bundle) string {
	if bundle == nil {
		return i18n.Default
	}

	if cookie, err := r.Cookie(LanguageCookie); err == nil && bundle.Supports(cookie.Value) {
		return cookie.Value
	}

	return bundle.Match(r.Header.Get("Accept-Language"))
}

// translationFuncs are the template functions of a language:
//
//	{{ t "nav.results" }}                     the message of a key
//	{{ t "greeting" "name" .Name }}           with placeholders filled from name/value pairs
//	{{ tn "laps" .Count }}                    the plural form for a count ({count} is set)
//	{{ tHTML "help.link" "url" .URL }}        a message which contains HTML; the values are escaped
//	{{ lang }}                                the language code, {{ languages }} all languages
func translationFuncs(bundle *i18n.Bundle, tr *i18n.Translator) template.FuncMap {
	return template.FuncMap{
		"t": func(key string, args ...interface{}) string {
			return tr.T(key, args...)
		},
		"tn": func(key string, n int, args ...interface{}) string {
			return tr.N(key, n, args...)
		},
		"tHTML": func(key string, args ...interface{}) template.HTML {
			return template.HTML(tr.T(key, escapeArgs(args)...))
		},
		"lang": func() string {
			return tr.Lang()
		},
		"languages": func() []i18n.Language {
			return bundle.Languages()
		},
		"jsMessages": func() map[string]string {
			return tr.Messages("js.")
		},
		"yn": func(b bool) string {
			if b {
				return tr.T("common.yes")
			}

			return tr.T("common.no")
		},
	}
}

// escapeArgs escapes the values of name/value pairs for HTML.
func escapeArgs(args []interface{}) []interface{} {
	out := make([]interface{}, len(args))

	for i, arg := range args {
		if i%2 == 1 {
			if html, ok := arg.(template.HTML); ok {
				out[i] = string(html)
				continue
			}

			out[i] = template.HTMLEscapeString(fmt.Sprint(arg))
			continue
		}

		out[i] = arg
	}

	return out
}

