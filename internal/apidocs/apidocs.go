// Package apidocs serves the OpenAPI specification of the JSON API (openapi.yaml), which is embedded in the binary.
package apidocs

import (
	"net/http"
)

// Pack the specification into this package
//go:generate esc -o apidocs_embed.go -pkg=apidocs openapi.yaml

// SpecPath is the path under which the specification is served.
const SpecPath = "/api/openapi.yaml"

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
