package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
)

// LanguageCookie remembers the language which was picked in the language selection.
const LanguageCookie = "lang"

// RequestLanguage returns the code of the language a request is answered in: the language picked by the account (on
// pages which know the account), else the one remembered in the language cookie, else the best match of the browser's
// Accept-Language header, else the default language.
func RequestLanguage(r *http.Request, bundle *i18n.Bundle) string {
	if bundle == nil {
		return i18n.Default
	}

	if lang := languageChoice(r, bundle); lang != "" {
		return lang
	}

	return bundle.Match(r.Header.Get("Accept-Language"))
}

type bundleContextKey struct{}

// WithBundle returns a copy of r which carries the message catalogs, so that handlers can translate texts (see T).
func WithBundle(r *http.Request, bundle *i18n.Bundle) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), bundleContextKey{}, bundle))
}

// TranslationMiddleware attaches the message catalogs to every request (see T).
func (tr *Renderer) TranslationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, WithBundle(r, tr.Bundle()))
	})
}

// T translates a message for the language of the request, e.g. for flash messages:
// smweb.AddFlash(w, r, smweb.T(r, "flash.saved", "name", name)). Without catalogs on the request (see
// TranslationMiddleware) it returns the key.
func T(r *http.Request, key string, args ...interface{}) string {
	bundle, _ := r.Context().Value(bundleContextKey{}).(*i18n.Bundle)

	if bundle == nil {
		return key
	}

	return bundle.Translator(RequestLanguage(r, bundle)).T(key, args...)
}

// languageChoice returns the language which was picked explicitly (account or cookie), empty if the browser language
// is used.
func languageChoice(r *http.Request, bundle *i18n.Bundle) string {
	if bundle == nil {
		return ""
	}

	if lang := AccountFromRequest(r).Language; lang != "" && bundle.Supports(lang) {
		return lang
	}

	if cookie, err := r.Cookie(LanguageCookie); err == nil && bundle.Supports(cookie.Value) {
		return cookie.Value
	}

	return ""
}

// languageCookieAge is how long the language cookie is kept (a year).
const languageCookieAge = 365 * 24 * 60 * 60

// RememberLanguage sets the language cookie, so that pages outside the login (and of anonymous visitors) are shown in
// the language. An empty code does nothing.
func RememberLanguage(w http.ResponseWriter, code string) {
	if code == "" {
		return
	}

	http.SetCookie(w, &http.Cookie{Name: LanguageCookie, Value: code, Path: "/", MaxAge: languageCookieAge, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// ForgetLanguage removes the language cookie, so that the browser language is used again.
func ForgetLanguage(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: LanguageCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

// translationFuncs are the template functions of a language:
//
//	{{ t "nav.results" }}                     the message of a key
//	{{ t "greeting" "name" .Name }}           with placeholders filled from name/value pairs
//	{{ tn "laps" .Count }}                    the plural form for a count ({count} is set)
//	{{ tHTML "help.link" "url" .URL }}        a message which contains HTML; the values are escaped
//	{{ lang }}                                the language code, {{ languages }} all languages
func translationFuncs(bundle *i18n.Bundle, tr *i18n.Translator) template.FuncMap {
	funcs := template.FuncMap{
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
		// {{ sessionName $sessionType }}: the name of a session type (serverconfig.SessionType or its string, e.g.
		// "RACE" or "Race" of a result)
		"sessionName": func(sessionType interface{}) string {
			var s serverconfig.SessionType

			switch v := sessionType.(type) {
			case serverconfig.SessionType:
				s = v
			case string:
				s = serverconfig.SessionType(strings.ToUpper(v))
			default:
				return fmt.Sprint(sessionType)
			}

			key := "session_type." + strings.ToLower(string(s))

			if msg := tr.T(key); msg != key {
				return msg
			}

			return s.String()
		},
		"yn": func(b bool) string {
			if b {
				return tr.T("common.yes")
			}

			return tr.T("common.no")
		},
	}

	for name, f := range (timeFormatter{tr: tr}).funcs() {
		funcs[name] = f
	}

	return funcs
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

