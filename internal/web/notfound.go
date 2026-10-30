package web

import (
	"net/http"
	"strings"
)

// NotFoundPageMiddleware replaces the plain text 404 responses (http.NotFound, http.Error with 404) that a browser
// navigation to a page receives with the output of page, which renders a styled 404 in the app layout and sets the
// status itself. Requests that are not page navigations (JSON APIs under /api/, assets, downloads, POSTs) keep the
// plain text response.
func NotFoundPageMiddleware(page http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !wantsHTMLPage(r) {
				next.ServeHTTP(w, r)
				return
			}

			nw := &notFoundWriter{ResponseWriter: w}
			next.ServeHTTP(nw, r)

			if nw.replaced {
				h := w.Header()
				h.Del("Content-Type")
				h.Del("Content-Length")
				h.Del("X-Content-Type-Options")

				page.ServeHTTP(w, r)
			}
		})
	}
}

func wantsHTMLPage(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}

	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

type notFoundWriter struct {
	http.ResponseWriter

	replaced    bool
	wroteHeader bool
}

func (w *notFoundWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}

	w.wroteHeader = true

	if code == http.StatusNotFound && strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		w.replaced = true
		return
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *notFoundWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	if w.replaced {
		return len(b), nil
	}

	return w.ResponseWriter.Write(b)
}

func (w *notFoundWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok && !w.replaced {
		f.Flush()
	}
}
