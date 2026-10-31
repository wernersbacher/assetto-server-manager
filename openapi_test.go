package servermanager

import (
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi"
	"gopkg.in/yaml.v2"

	"github.com/wernersbacher/ACPitwall/internal/apidocs"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// specOperations returns "METHOD /path" for every operation of the embedded OpenAPI document.
func specOperations(t *testing.T) map[string]bool {
	t.Helper()

	raw, err := apidocs.Spec()

	if err != nil {
		t.Fatal(err)
	}

	var spec struct {
		Paths map[string]map[string]interface{} `yaml:"paths"`
	}

	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("openapi.yaml is not valid YAML: %s", err)
	}

	methods := map[string]bool{"get": true, "put": true, "post": true, "delete": true, "patch": true, "head": true, "options": true}
	ops := make(map[string]bool)

	for path, item := range spec.Paths {
		for key := range item {
			if methods[key] {
				ops[strings.ToUpper(key)+" "+path] = true
			}
		}
	}

	return ops
}

// The API documentation is maintained by hand, so this test keeps it in sync with the router: every /api route has
// to be described in openapi.yaml (with the same path parameter names) and every described operation must exist.
func TestOpenAPISpecMatchesRouter(t *testing.T) {
	dir, err := ioutil.TempDir("", "openapi-test")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "shared"))

	resolver, err := NewResolver(&smconfig.Configuration{}, smweb.NewFilesystemTemplateLoader(filepath.Join("cmd", "acpitwall", "views")), false, store, serverenv.NewState())

	if err != nil {
		t.Fatal(err)
	}

	router, ok := resolver.ResolveRouter(http.Dir(dir)).(chi.Routes)

	if !ok {
		t.Fatal("router is not a chi.Routes")
	}

	routes := make(map[string]bool)

	err = chi.Walk(router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		// asset mounts like /api/docs/* are not API operations
		if strings.HasPrefix(route, "/api/") && !strings.HasSuffix(route, "/*") {
			routes[method+" "+route] = true
		}

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	spec := specOperations(t)

	var undocumented, unrouted []string

	for route := range routes {
		if !spec[route] {
			undocumented = append(undocumented, route)
		}
	}

	for op := range spec {
		if !routes[op] {
			unrouted = append(unrouted, op)
		}
	}

	sort.Strings(undocumented)
	sort.Strings(unrouted)

	if len(undocumented) > 0 {
		t.Errorf("routes missing in internal/apidocs/openapi.yaml: %s", strings.Join(undocumented, ", "))
	}

	if len(unrouted) > 0 {
		t.Errorf("operations in internal/apidocs/openapi.yaml without a route: %s", strings.Join(unrouted, ", "))
	}
}
