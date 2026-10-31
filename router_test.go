package servermanager

import (
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"

	"io/ioutil"
	"net/http"
	"net/http/httptest"
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

	resolver, err := NewResolver(&smconfig.Configuration{}, smweb.NewFilesystemTemplateLoader(filepath.Join("cmd", "acpitwall", "views")), false, store, serverenv.NewState())

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

// unknown /api/ paths keep the plain text 404 even for clients that accept HTML, only page navigations get the styled page
func TestRouterUnknownAPIPathIsPlainText(t *testing.T) {
	dir, err := ioutil.TempDir("", "router-test")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "shared"))

	resolver, err := NewResolver(&smconfig.Configuration{}, smweb.NewFilesystemTemplateLoader(filepath.Join("cmd", "acpitwall", "views")), false, store, serverenv.NewState())

	if err != nil {
		t.Fatal(err)
	}

	router := resolver.ResolveRouter(http.Dir(dir))

	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	req.Header.Set("Accept", "text/html")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("expected a plain text 404, got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// state-changing actions (start, stop, cancel, ...) must only be reachable via POST
func TestRouterActionRoutesArePostOnly(t *testing.T) {
	dir, err := ioutil.TempDir("", "router-test")

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
		t.Fatal("router does not implement chi.Routes")
	}

	methods := make(map[string][]string)

	err = chi.Walk(router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		methods[route] = append(methods[route], method)

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	for _, route := range []string{
		"/process/{action}",
		"/custom/load/{uuid}",
		"/custom/schedule/{uuid}/remove",
		"/custom/star/{uuid}",
		"/custom/loop/{uuid}",
		"/accounts/toggle-open",
		"/accounts/toggle-public-stats",
		"/championship/{championshipID}/duplicate",
		"/championship/{championshipID}/event/{eventID}/start",
		"/championship/{championshipID}/event/{eventID}/practice",
		"/championship/{championshipID}/event/{eventID}/cancel",
		"/championship/{championshipID}/event/{eventID}/finish",
		"/championship/{championshipID}/event/{eventID}/restart",
		"/championship/{championshipID}/event/{eventID}/schedule/remove",
		"/championship/{championshipID}/event/{eventID}/duplicate",
		"/championship/{championshipID}/entrant/{entrantGUID}",
		"/championship/{championshipID}/custom/{eventID}/import",
		"/championship/{championshipID}/race-weekend/{weekendID}/import",
		"/race-weekend/{raceWeekendID}/session/{sessionID}/start",
		"/race-weekend/{raceWeekendID}/session/{sessionID}/practice",
		"/race-weekend/{raceWeekendID}/session/{sessionID}/restart",
		"/race-weekend/{raceWeekendID}/session/{sessionID}/cancel",
		"/race-weekend/{raceWeekendID}/session/{sessionID}/schedule/remove",
		"/race-weekend/{raceWeekendID}/update-entrylist",
	} {
		got, ok := methods[route]

		if !ok {
			t.Errorf("route %s is not registered", route)
			continue
		}

		if len(got) != 1 || got[0] != http.MethodPost {
			t.Errorf("route %s must be POST only, is registered for %v", route, got)
		}
	}
}
