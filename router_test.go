package servermanager

import (
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"

	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi"
)

// destructive routes must not be reachable via GET, otherwise link prefetching or a
// cross-site <img src="..."> can delete content
func TestRouterDeleteRoutesArePostOnly(t *testing.T) {

	dir, err := ioutil.TempDir("", "router-test")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "shared"))

	resolver, err := NewResolver(&smconfig.Configuration{}, NewFilesystemTemplateLoader(filepath.Join("cmd", "server-manager", "views")), false, store)

	if err != nil {
		t.Fatal(err)
	}

	router, ok := resolver.ResolveRouter(http.Dir(dir)).(chi.Routes)

	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	found := 0

	err = chi.Walk(router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if !strings.Contains(route, "/delete") {
			return nil
		}

		found++

		if method != http.MethodPost {
			t.Errorf("delete route %s is reachable via %s", route, method)
		}

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	if found == 0 {
		t.Fatal("no delete routes found")
	}
}
