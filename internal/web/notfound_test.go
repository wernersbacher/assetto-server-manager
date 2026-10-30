package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotFoundPageMiddleware(t *testing.T) {
	page := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<html>styled</html>"))
	})

	h := NotFoundPageMiddleware(page)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("fine"))
		case "/json":
			http.Error(w, `{"error":"x"}`, http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))

	do := func(method, path, accept string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)

		if accept != "" {
			req.Header.Set("Accept", accept)
		}

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		return rec
	}

	if rec := do("GET", "/missing", "text/html,*/*"); rec.Code != 404 || rec.Body.String() != "<html>styled</html>" {
		t.Errorf("browser navigation: got %d %q", rec.Code, rec.Body.String())
	}

	if rec := do("GET", "/api/thing", "text/html"); rec.Code != 404 || rec.Body.String() != "404 page not found\n" {
		t.Errorf("api: got %d %q", rec.Code, rec.Body.String())
	}

	if rec := do("GET", "/missing", "application/json"); rec.Code != 404 || rec.Body.String() != "404 page not found\n" {
		t.Errorf("json client: got %d %q", rec.Code, rec.Body.String())
	}

	if rec := do("POST", "/missing", "text/html"); rec.Body.String() != "404 page not found\n" {
		t.Errorf("post: got %q", rec.Body.String())
	}

	if rec := do("GET", "/ok", "text/html"); rec.Code != 200 || rec.Body.String() != "fine" {
		t.Errorf("ok page: got %d %q", rec.Code, rec.Body.String())
	}
}
