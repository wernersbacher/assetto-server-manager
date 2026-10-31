// Package apidocs serves the OpenAPI specification of the JSON API (openapi.yaml) and a Swagger UI for it. Both are
// embedded in the binary, so the documentation works without internet access.
package apidocs

import (
	"net/http"
)

// Pack the specification and the Swagger UI (swagger-ui-dist, see swagger-ui/LICENSE) into this package
//go:generate esc -o apidocs_embed.go -pkg=apidocs openapi.yaml swagger-ui

const (
	// SpecPath is the path under which the specification is served.
	SpecPath = "/api/openapi.yaml"

	// UIPath is the path of the Swagger UI page, UIAssetsPattern the route of its scripts and styles.
	UIPath          = "/api/docs"
	UIAssetsPattern = UIPath + "/*"
)

// Spec returns the embedded OpenAPI document.
func Spec() ([]byte, error) {
	return FSByte(false, "/openapi.yaml")
}

// SpecHandler serves the OpenAPI document.
func SpecHandler(w http.ResponseWriter, r *http.Request) {
	spec, err := Spec()

	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(spec)
}

// UIHandler serves the Swagger UI page.
func UIHandler(w http.ResponseWriter, r *http.Request) {
	page, err := FSByte(false, "/swagger-ui/index.html")

	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

// UIAssetsHandler serves the scripts and styles of the Swagger UI below UIPath.
func UIAssetsHandler() http.Handler {
	return http.StripPrefix(UIPath+"/", http.FileServer(Dir(false, "/swagger-ui")))
}
