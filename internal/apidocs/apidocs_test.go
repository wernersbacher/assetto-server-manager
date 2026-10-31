package apidocs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi"
)

func TestSwaggerUIIsServedFromEmbeddedAssets(t *testing.T) {
	r := chi.NewRouter()
	r.Get(SpecPath, SpecHandler)
	r.Get(UIPath, UIHandler)
	r.Get(UIAssetsPattern, UIAssetsHandler().ServeHTTP)

	for path, want := range map[string]string{
		SpecPath:                           "openapi: 3.",
		UIPath:                             "swagger-ui-bundle.js",
		UIPath + "/swagger-ui-bundle.js":   "SwaggerUIBundle",
		UIPath + "/swagger-ui.css":         ".swagger-ui",
		UIPath + "/swagger-initializer.js": SpecPath,
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", path, rec.Code)
		}

		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: body does not contain %q", path, want)
		}
	}
}
