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
	"reflect"
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
		"/accounts/download-access",
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

func TestNoDirListing(t *testing.T) {
	dir, err := ioutil.TempDir("", "nodirlisting")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	if err := os.MkdirAll(filepath.Join(dir, "cars", "x"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(dir, "cars", "x", "ui.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	handler := http.FileServer(noDirListing{http.Dir(dir)})

	for target, want := range map[string]int{"/": http.StatusNotFound, "/cars/": http.StatusNotFound, "/cars/x/ui.json": http.StatusOK} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

		if rec.Code != want {
			t.Errorf("%s: expected %d, got %d", target, want, rec.Code)
		}
	}
}

// deleting a race weekend needs delete access like every other delete, so its route must be
// in the same middleware group as the session delete and not in the write group
func TestRouterRaceWeekendDeleteNeedsDeleteAccess(t *testing.T) {
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

	chains := make(map[string][]uintptr)

	err = chi.Walk(router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if method == http.MethodPost {
			var ptrs []uintptr

			for _, mw := range middlewares {
				ptrs = append(ptrs, reflect.ValueOf(mw).Pointer())
			}

			chains[route] = ptrs
		}

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	weekend, ok := chains["/race-weekend/{raceWeekendID}/delete"]

	if !ok {
		t.Fatal("race weekend delete route is not registered")
	}

	if session := chains["/race-weekend/{raceWeekendID}/session/{sessionID}/delete"]; !reflect.DeepEqual(weekend, session) {
		t.Errorf("race weekend delete is not in the delete access group (middlewares %v, session delete %v)", weekend, session)
	}

	if write := chains["/race-weekends/new/submit"]; reflect.DeepEqual(weekend, write) {
		t.Errorf("race weekend delete is in the write access group")
	}
}
