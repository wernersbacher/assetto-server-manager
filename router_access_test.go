package servermanager

import (
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"

	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi"
)

// How a route is guarded. Every route of the router has to be listed in routeKinds (TestEveryRouteHasAnAccessRule), so
// that a new route cannot go live without a decision about who may use it.
type accessKind int

const (
	// kindArea: guarded by the level of the area (AreaAccessMiddleware).
	kindArea accessKind = iota
	// kindAreaLogin: needs the area and a login, because the handler acts on behalf of an account (team actions,
	// championship sign up). The handler redirects visitors without an account to the login, also in a public area.
	kindAreaLogin
	// kindAnyArea: no area of its own, open for anonymous visitors as long as any area is public (home, changelog,
	// API documentation, event details whose handler checks the area of what it shows).
	kindAnyArea
	// kindLogin: any logged in account (account management, skins of users).
	kindLogin
	// kindWrite, kindDelete and kindAdmin: the access groups. No area is involved.
	kindWrite
	kindDelete
	kindAdmin
	// kindOpen: deliberately without any check (login page, static files, monitoring).
	kindOpen
)

type accessRule struct {
	kind accessKind
	area smmodel.Area
}

// routeKinds is the access rule of every route, as "METHOD pattern" (ANY for a route registered for all methods).
// The comments say why a route belongs where it does.
var routeKinds = func() map[string]accessRule {
	rules := make(map[string]accessRule)

	add := func(rule accessRule, routes ...string) {
		for _, route := range routes {
			if _, ok := rules[route]; ok {
				panic("route listed twice: " + route)
			}

			rules[route] = rule
		}
	}

	area := func(a smmodel.Area, routes ...string) { add(accessRule{kindArea, a}, routes...) }
	areaLogin := func(a smmodel.Area, routes ...string) { add(accessRule{kindAreaLogin, a}, routes...) }
	other := func(k accessKind, routes ...string) { add(accessRule{kind: k}, routes...) }

	// driver data: profiles, leaderboards, lap records and the stracker web interface (lap times and driver stats)
	area(smmodel.AreaStats,
		"GET /api/track-records", "GET /driver/{guid}", "GET /api/driver/{guid}", "GET /leaderboards",
		"GET /api/leaderboard", "ANY /stracker/*", // the whole proxy, all methods
	)

	// cars, tracks, weather and car classes, including the pictures and ui json the pages show
	area(smmodel.AreaContent,
		"GET /cars", "GET /cars/search.json", "GET /car/{car_id}", "GET /car-classes", "GET /car-classes/{classID}",
		"GET /api/car-classes", "GET /api/car-classes/{classID}", "GET /tracks", "GET /track/{track_id}", "GET /weather",
		"GET /content/tracks/{track}/ui/preview.png", "GET /content/tracks/{track}/ui/{layout}/preview.png",
		"GET /content/tracks/{track}/ui/ui_track.json", "GET /content/tracks/{track}/ui/{layout}/ui_track.json",
	)

	// the calendar and every feed of upcoming races (the iCal feed of one championship belongs to its area, because
	// it follows the championship and can be switched off with it)
	area(smmodel.AreaCalendar, "GET /events.ics", "GET /calendar", "GET /calendar.json", "GET /api/upcoming-races")

	// the live view, its websocket and the status of the servers
	area(smmodel.AreaLiveTiming, "GET /live-timing", "GET /api/race-control", "GET /api/server-status", "GET /api/servers")

	// results with their downloads (the json files of acServer) and the API
	area(smmodel.AreaResults,
		"GET /results", "GET /results/{fileName}", "ANY /results/{fileName}/collisions", "ANY /results/download/{fileName}",
		"GET /api/results", "GET /api/results/{fileName}",
	)

	area(smmodel.AreaCustomRaces, "GET /custom")

	// teams: listing and viewing are guarded by the area, the actions need an account as well
	area(smmodel.AreaTeams, "GET /teams", "GET /api/teams", "GET /api/team/{teamID}", "GET /teams/{teamID}", "GET /teams/invite/{token}")
	areaLogin(smmodel.AreaTeams,
		"POST /teams/invite/{token}", "POST /teams/new", "POST /teams/{teamID}/join",
		"POST /teams/{teamID}/leave", "POST /teams/{teamID}/decline", "POST /teams/{teamID}/rename",
		"POST /teams/{teamID}/visibility", "POST /teams/{teamID}/invite", "POST /teams/{teamID}/invite/revoke",
		"POST /teams/{teamID}/members/{accountID}/kick", "POST /teams/{teamID}/members/{accountID}/admin",
		"POST /teams/{teamID}/members/{accountID}/transfer", "POST /teams/{teamID}/delete",
	)

	// championships: the pages, the API and the exports (they contain standings and results, so they are not part
	// of the results or stats areas but follow the championship)
	area(smmodel.AreaChampionships,
		"GET /championships", "GET /championship/{championshipID}", "GET /championship/{championshipID}/export",
		"GET /api/championships", "GET /api/championship/{championshipID}", "GET /api/championship/{championshipID}/standings",
		"ANY /championship/{championshipID}/export-results", "GET /championship/{championshipID}/ics",
	)
	areaLogin(smmodel.AreaChampionships, "GET /championship/{championshipID}/sign-up", "POST /championship/{championshipID}/sign-up")

	area(smmodel.AreaRaceWeekends,
		"GET /race-weekends", "GET /race-weekend/{raceWeekendID}", "GET /race-weekend/{raceWeekendID}/filters",
		"GET /race-weekend/{raceWeekendID}/entrylist", "POST /race-weekend/{raceWeekendID}/grid-preview",
		"GET /race-weekend/{raceWeekendID}/entrylist-preview", "GET /race-weekend/{raceWeekendID}/export",
	)

	// downloads: the content directory (except the pictures and ui json, which follow content), setups and skin zips
	area(smmodel.AreaDownloads,
		"GET /content", "GET /content/*", "GET /setups/download", "GET /setups/download/*",
		"GET /car/{car_id}/skin/{skin}/download",
	)

	// no area: entry pages, the popup of an event (its handler checks the area of the event), the 404 page
	other(kindAnyArea,
		"GET /", "GET /changelog", "GET /api/openapi.yaml", "GET /api/docs", "GET /api/docs/*", "GET /event-details",
	)

	// logged in accounts: their own account and skins (the skin options and ownership are checked in the handlers),
	// the preview of private and team skins
	other(kindLogin,
		"ANY /accounts/new-password", "ANY /accounts/update", "GET /accounts/steam-link/finish", "POST /accounts/delete-own",
		"ANY /accounts/dismiss-changelog", "POST /car/{name}/skin", "POST /car/{name}/skin/visibility",
		"POST /car/{name}/skin/remove", "GET /car/{car_id}/skin/{skin}/preview.jpg",
	)

	// deliberately open: the login, language, static files, monitoring (the Steam link flow is started anonymously)
	other(kindOpen,
		"ANY /login", "GET /login/steam", "ANY /logout", "POST /language", "ANY /robots.txt", "ANY /metrics",
		"GET /healthcheck.json", "GET /accounts/steam-link", "GET /static", "GET /static/*",
	)

	other(kindWrite,
		"POST /setups/upload", "ANY /car/{name}/tags", "GET /car-classes/new", "POST /car-classes/new",
		"POST /car-classes/{classID}", "POST /car-classes/{classID}/duplicate", "POST /car/{name}/metadata",
		"POST /track/{name}/metadata", "POST /results/upload", "ANY /results/combine", "GET /quick", "POST /quick/submit",
		"GET /custom/new", "POST /custom/load/{uuid}", "POST /custom/schedule/{uuid}", "POST /custom/schedule/{uuid}/remove",
		"GET /custom/edit/{uuid}", "POST /custom/star/{uuid}", "POST /custom/loop/{uuid}", "POST /custom/new/submit",
		"GET /custom/import", "POST /custom/import", "GET /custom/export/{uuid}", "POST /process/{action}", "GET /logs",
		"GET /api/logs", "GET /api/log-download/{logFile}",
		"GET /championships/new", "POST /championships/new/submit", "GET /championship/{championshipID}/edit",
		"POST /championship/{championshipID}/duplicate", "GET /championship/{championshipID}/event",
		"POST /championship/{championshipID}/event/submit", "POST /championship/{championshipID}/event/{eventID}/start",
		"POST /championship/{championshipID}/event/{eventID}/schedule",
		"POST /championship/{championshipID}/event/{eventID}/schedule/remove",
		"GET /championship/{championshipID}/event/{eventID}/edit",
		"POST /championship/{championshipID}/event/{eventID}/practice",
		"POST /championship/{championshipID}/event/{eventID}/cancel", "POST /championship/{championshipID}/event/{eventID}/finish",
		"POST /championship/{championshipID}/event/{eventID}/restart", "POST /championship/{championshipID}/event/{eventID}/duplicate",
		"POST /championship/{championshipID}/driver-penalty/{classID}/{driverGUID}",
		"POST /championship/{championshipID}/team-penalty/{classID}/{team}", "GET /championship/{championshipID}/entrants",
		"GET /championship/{championshipID}/entrants.csv", "POST /championship/{championshipID}/entrant/{entrantGUID}",
		"POST /championship/{championshipID}/reorder-events", "GET /championship/import", "POST /championship/import",
		"GET /championship/{championshipID}/event/{eventID}/import", "POST /championship/{championshipID}/event/{eventID}/import",
		"GET /championship/{championshipID}/custom/list", "POST /championship/{championshipID}/custom/{eventID}/import",
		"GET /championship/{championshipID}/race-weekend/list", "POST /championship/{championshipID}/race-weekend/{weekendID}/import",
		"POST /penalties/{sessionFile}/{driverGUID}", "POST /results/{fileName}/edit", "POST /live-timing/save-frames",
		"POST /api/track/upload", "POST /api/car/upload", "POST /api/weather/upload", "POST /api/image-upload",
		"GET /race-weekends/new", "POST /race-weekends/new/submit", "GET /race-weekend/{raceWeekendID}/edit",
		"GET /race-weekend/{raceWeekendID}/session", "POST /race-weekend/{raceWeekendID}/session/submit",
		"GET /race-weekend/{raceWeekendID}/session/{sessionID}/edit", "POST /race-weekend/{raceWeekendID}/session/{sessionID}/start",
		"POST /race-weekend/{raceWeekendID}/session/{sessionID}/practice", "POST /race-weekend/{raceWeekendID}/session/{sessionID}/restart",
		"POST /race-weekend/{raceWeekendID}/session/{sessionID}/cancel", "GET /race-weekend/{raceWeekendID}/session/{sessionID}/import",
		"POST /race-weekend/{raceWeekendID}/session/{sessionID}/import", "POST /race-weekend/{raceWeekendID}/update-grid",
		"POST /race-weekend/{raceWeekendID}/update-entrylist", "GET /race-weekend/import", "POST /race-weekend/import",
		"POST /race-weekend/{raceWeekendID}/session/{sessionID}/schedule",
		"POST /race-weekend/{raceWeekendID}/session/{sessionID}/schedule/remove",
	)

	other(kindDelete,
		"POST /championship/{championshipID}/event/{eventID}/delete", "POST /championship/{championshipID}/delete",
		"POST /custom/delete/{uuid}", "POST /results/{fileName}/delete", "POST /track/{name}/delete", "POST /car/{name}/delete",
		"POST /car-classes/{classID}/delete", "POST /car/{name}/skin/delete", "POST /weather/delete/{key}",
		"POST /setups/delete/{car}/{track}/{setup}", "GET /autofill-entrants", "POST /autofill-entrants/delete/{entrantID}",
		"POST /race-weekend/{raceWeekendID}/delete", "POST /race-weekend/{raceWeekendID}/session/{sessionID}/delete",
	)

	other(kindAdmin,
		"GET /servers", "ANY /server-options", "ANY /blacklist", "ANY /motd", "ANY /current-config", "ANY /audit-logs",
		"ANY /accounts/new", "ANY /accounts/edit/{id}", "POST /accounts/delete/{id}", "ANY /accounts/reset-password/{id}",
		"POST /accounts/visibility", "POST /accounts/toggle-steam-signup", "POST /accounts/team-options",
		"POST /accounts/skin-options", "ANY /accounts", "POST /search-index",
		"ANY /restart-session", "ANY /next-session", "ANY /broadcast-chat", "ANY /admin-command", "ANY /kick-user",
		"ANY /send-chat", "ANY /countdown", "ANY /stracker/options", "ANY /kissmyrank/options", "ANY /realpenalty/options",
		"ANY /realpenalty/logs",
	)

	return rules
}()

// routeRule finds the rule of a walked route: the exact "METHOD pattern" first, then the "ANY pattern" of routes
// registered for all methods.
func routeRule(method, pattern string) (accessRule, bool) {
	if rule, ok := routeKinds[method+" "+pattern]; ok {
		return rule, true
	}

	rule, ok := routeKinds["ANY "+pattern]

	return rule, ok
}

// walkRoutes lists the "METHOD pattern" of every route of the router (HEAD, OPTIONS, CONNECT and TRACE, which chi
// adds for routes registered for all methods, are left out: they share the middleware of the other methods).
func walkRoutes(t *testing.T, router http.Handler) []string {
	t.Helper()

	routes, ok := router.(chi.Routes)

	if !ok {
		t.Fatal("router does not implement chi.Routes")
	}

	var found []string

	err := chi.Walk(routes, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace:
			return nil
		}

		found = append(found, method+" "+route)

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(found)

	return found
}

// every route needs a decision about who may use it, so that a new route cannot be forgotten in the wrong group
func TestEveryRouteHasAnAccessRule(t *testing.T) {
	env := newAccessTestEnv(t)
	defer env.cleanup()

	seen := make(map[string]bool)

	for _, route := range walkRoutes(t, env.router) {
		parts := strings.SplitN(route, " ", 2)

		if _, ok := routeRule(parts[0], parts[1]); !ok {
			t.Errorf("route %s has no access rule: add it to routeKinds in router_access_test.go (after choosing its area or group in router.go)", route)
			continue
		}

		seen[parts[0]+" "+parts[1]] = true
		seen["ANY "+parts[1]] = true
	}

	// a rule without a route is a typo or a removed route
	for route := range routeKinds {
		if !seen[route] {
			t.Errorf("access rule %q has no route", route)
		}
	}
}

type accessTestEnv struct {
	t       *testing.T
	router  http.Handler
	cleanup func()

	// cookies of the logged in accounts, by actor
	cookies map[string][]*http.Cookie

	// championshipID is a championship with an open sign up
	championshipID string
}

// the actors of the tests, in the order of their rights
var accessActors = []string{"anonymous", "read", "write", "admin"}

// accessRank is how far the actor reaches: it may see a level if the rank is at least the level (public is 0).
var accessRank = map[string]int{"anonymous": 0, "read": 1, "write": 2, "admin": 3}

func newAccessTestEnv(t *testing.T) *accessTestEnv {
	t.Helper()

	dir, err := ioutil.TempDir("", "router-access")

	if err != nil {
		t.Fatal(err)
	}

	resolver, store, cleanupResolver := newTestResolver(t, &smconfig.Configuration{})

	if err := resolver.state.SetInstallPath(dir); err != nil {
		t.Fatal(err)
	}

	for _, sub := range []string{"content/cars", "content/tracks", "content/weather", "setups", "results"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(sub)), 0755); err != nil {
			t.Fatal(err)
		}
	}

	carManager := resolver.resolveCarManager()

	if err := carManager.CreateOrOpenSearchIndex(); err != nil {
		t.Fatal(err)
	}

	accountManager := resolver.resolveAccountManager()
	accountManager.Init(teamsTestServerID)

	env := &accessTestEnv{
		t:       t,
		router:  resolver.ResolveRouter(http.Dir(dir)),
		cookies: make(map[string][]*http.Cookie),
		cleanup: func() {
			_ = carManager.CloseSearchIndex()
			cleanupResolver()
			_ = os.RemoveAll(dir)
		},
	}

	for actor, group := range map[string]smmodel.Group{"read": smmodel.GroupRead, "write": smmodel.GroupWrite, "admin": smmodel.GroupAdmin} {
		account := smmodel.NewAccount(teamsTestServerID, "test")
		account.Name = "access-" + actor
		account.Groups[teamsTestServerID] = group

		if err := accountManager.ChangePassword(account, "secret"); err != nil {
			t.Fatal(err)
		}

		if err := store.UpsertAccount(account); err != nil {
			t.Fatal(err)
		}

		rec := env.do(http.MethodPost, "/login", nil, url.Values{"Username": {account.Name}, "Password": {"secret"}})

		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
			t.Fatalf("login of %s: %d %q", actor, rec.Code, rec.Header().Get("Location"))
		}

		env.cookies[actor] = rec.Result().Cookies()
	}

	championship := smmodel.NewChampionship("Access test")
	class := smmodel.NewChampionshipClass("Default")
	class.Entrants = serverconfig.EntryList{}

	for i := 0; i < 4; i++ {
		class.Entrants.AddToBackOfGrid(serverconfig.NewEntrant())
	}

	championship.AddClass(class)
	championship.SignUpForm = smmodel.ChampionshipSignUpForm{Enabled: true, HideCarChoice: true}

	if err := store.UpsertChampionship(championship); err != nil {
		t.Fatal(err)
	}

	env.championshipID = championship.ID.String()

	return env
}

func (e *accessTestEnv) do(method, target string, cookies []*http.Cookie, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)

	return rec
}

// request is do as one of the actors.
func (e *accessTestEnv) request(actor, method, target string) *httptest.ResponseRecorder {
	e.t.Helper()

	return e.do(method, target, e.cookies[actor], url.Values{})
}

// setLevel sets the level of the area through the admin page, like an admin does.
func (e *accessTestEnv) setLevel(area smmodel.Area, level smmodel.Level) {
	e.t.Helper()

	rec := e.do(http.MethodPost, "/accounts/visibility", e.cookies["admin"], url.Values{"area": {string(area)}, "level": {level.String()}})

	if rec.Code != http.StatusFound && rec.Code != http.StatusOK {
		e.t.Fatalf("set %s to %s: %d", area, level, rec.Code)
	}
}

// setAll gives every area the same level.
func (e *accessTestEnv) setAll(level smmodel.Level) {
	for _, area := range smmodel.AllAreas {
		e.setLevel(area, level)
	}
}

// concretePath fills the parameters of a route pattern.
func concretePath(pattern string) string {
	var out strings.Builder

	inParam := false

	for _, c := range pattern {
		switch {
		case c == '{':
			inParam = true
			out.WriteString("x")
		case c == '}':
			inParam = false
		case inParam:
		case c == '*':
			out.WriteString("x")
		default:
			out.WriteRune(c)
		}
	}

	return out.String()
}

// pathsFor returns the concrete paths to request for a pattern. The downloads and content routes share /content/*,
// whose area depends on the file: the pictures and ui json follow content.
func pathsFor(rule accessRule, pattern string) []string {
	if pattern == "/content/*" {
		return []string{"/content/cars/x/data.acd"}
	}

	return []string{concretePath(pattern)}
}

// denied tells whether the response is what the access middlewares answer: a redirect to the login (visitors) or
// to the home page (accounts with too low access).
func denied(rec *httptest.ResponseRecorder) bool {
	if rec.Code != http.StatusFound {
		return false
	}

	loc := rec.Header().Get("Location")

	return loc == "/login" || loc == "/"
}

// walkedRule is a route of the router with its rule.
type walkedRule struct {
	method, pattern string
	rule            accessRule
}

func rulesOf(t *testing.T, env *accessTestEnv, filter func(walkedRule) bool) []walkedRule {
	t.Helper()

	var out []walkedRule

	for _, route := range walkRoutes(t, env.router) {
		parts := strings.SplitN(route, " ", 2)
		rule, ok := routeRule(parts[0], parts[1])

		if !ok {
			continue
		}

		w := walkedRule{parts[0], parts[1], rule}

		if filter(w) {
			out = append(out, w)
		}
	}

	return out
}

// every page and API of an area is open exactly for the actors the level of the area lets in
func TestRouterAreaLevels(t *testing.T) {
	env := newAccessTestEnv(t)
	defer env.cleanup()

	env.setAll(smmodel.LevelPublic)

	for _, area := range smmodel.AllAreas {
		routes := rulesOf(t, env, func(w walkedRule) bool {
			return w.rule.kind == kindArea && w.rule.area == area && w.method == http.MethodGet
		})

		if len(routes) == 0 {
			t.Errorf("area %s has no GET route", area)
			continue
		}

		for _, level := range smmodel.AllLevels {
			env.setLevel(area, level)

			for _, actor := range accessActors {
				allowed := accessRank[actor] >= int(level)

				for _, w := range routes {
					for _, path := range pathsFor(w.rule, w.pattern) {
						rec := env.request(actor, w.method, path)

						if allowed && denied(rec) {
							t.Errorf("area %s level %s: %s %s was refused for %s (%d %s)", area, level, w.method, path, actor, rec.Code, rec.Header().Get("Location"))
						}

						if !allowed {
							if !denied(rec) {
								t.Errorf("area %s level %s: %s %s was served to %s (%d)", area, level, w.method, path, actor, rec.Code)
							} else if actor == "anonymous" && rec.Header().Get("Location") != "/login" {
								t.Errorf("area %s level %s: anonymous visitor of %s was sent to %q, not to the login", area, level, path, rec.Header().Get("Location"))
							}
						}
					}
				}
			}

			env.setLevel(area, smmodel.LevelPublic)
		}
	}
}

// the skin zip of the downloads area and the pictures of the content area are different cases of /content
func TestRouterContentFilesFollowTheirArea(t *testing.T) {
	env := newAccessTestEnv(t)
	defer env.cleanup()

	env.setAll(smmodel.LevelPublic)
	env.setLevel(smmodel.AreaDownloads, smmodel.LevelLoggedIn)

	if rec := env.request("anonymous", http.MethodGet, "/content/cars/x/data.acd"); !denied(rec) {
		t.Errorf("a file of the content directory was served to an anonymous visitor with downloads for accounts only (%d)", rec.Code)
	}

	for _, path := range []string{"/content/cars/x/ui/ui_car.json", "/content/cars/x/skins/y/preview.jpg"} {
		if rec := env.request("anonymous", http.MethodGet, path); denied(rec) {
			t.Errorf("%s follows the content area and was refused, although it is public", path)
		}
	}

	env.setLevel(smmodel.AreaDownloads, smmodel.LevelPublic)
	env.setLevel(smmodel.AreaContent, smmodel.LevelLoggedIn)

	if rec := env.request("anonymous", http.MethodGet, "/content/cars/x/ui/ui_car.json"); !denied(rec) {
		t.Errorf("the ui json of a car was served to an anonymous visitor with content for accounts only (%d)", rec.Code)
	}

	if rec := env.request("anonymous", http.MethodGet, "/content/cars/x/data.acd"); denied(rec) {
		t.Error("a file of the content directory was refused although downloads are public")
	}

	// skin zips: no signature, so the downloads area decides
	env.setLevel(smmodel.AreaDownloads, smmodel.LevelLoggedIn)

	if rec := env.request("anonymous", http.MethodGet, "/car/x/skin/y/download"); !denied(rec) {
		t.Errorf("a skin zip was served to an anonymous visitor with downloads for accounts only (%d)", rec.Code)
	}
}

// actions that act for an account never work anonymously, also in a public area: the handlers refuse them, the
// middleware only lets accounts in that may see the area
func TestRouterAccountActionsNeedALogin(t *testing.T) {
	env := newAccessTestEnv(t)
	defer env.cleanup()

	env.setAll(smmodel.LevelPublic)

	routes := rulesOf(t, env, func(w walkedRule) bool { return w.rule.kind == kindAreaLogin })

	if len(routes) < 15 {
		t.Fatalf("expected the team and sign up routes, got %v", routes)
	}

	for _, w := range routes {
		path := concretePath(strings.Replace(w.pattern, "{championshipID}", env.championshipID, 1))

		if rec := env.request("anonymous", w.method, path); !denied(rec) || rec.Header().Get("Location") != "/login" {
			t.Errorf("%s %s: expected a redirect to the login for an anonymous visitor, got %d %q", w.method, path, rec.Code, rec.Header().Get("Location"))
		}
	}

	// and an account still needs the area
	for _, w := range routes {
		env.setLevel(w.rule.area, smmodel.LevelAdmin)
	}

	for _, w := range routes {
		path := concretePath(strings.Replace(w.pattern, "{championshipID}", env.championshipID, 1))

		if rec := env.request("read", w.method, path); !denied(rec) {
			t.Errorf("%s %s was served to a reader although the area %s is for admins (%d)", w.method, path, w.rule.area, rec.Code)
		}
	}
}

// every route outside of the areas needs its group, whatever the visibility of the areas is
func TestRouterGroupRoutesNeedTheirGroup(t *testing.T) {
	env := newAccessTestEnv(t)
	defer env.cleanup()

	// all areas public: this must not open any route of the groups
	env.setAll(smmodel.LevelPublic)

	minimum := map[accessKind]int{kindLogin: 1, kindWrite: 2, kindDelete: 3, kindAdmin: 3}

	// the reader and the writer are below the delete group
	rank := map[string]int{"anonymous": 0, "read": 1, "write": 2, "admin": 3}

	routes := rulesOf(t, env, func(w walkedRule) bool {
		_, ok := minimum[w.rule.kind]

		return ok
	})

	for _, w := range routes {
		path := concretePath(strings.Replace(w.pattern, "{championshipID}", env.championshipID, 1))

		for _, actor := range accessActors {
			if rank[actor] >= minimum[w.rule.kind] {
				continue
			}

			if rec := env.request(actor, w.method, path); !denied(rec) {
				t.Errorf("%s %s was served to %s (%d)", w.method, path, actor, rec.Code)
			}
		}
	}
}

// the entry pages are open while any area is public and the whole server is closed when no area is
func TestRouterEntryPagesFollowTheAreas(t *testing.T) {
	env := newAccessTestEnv(t)
	defer env.cleanup()

	entries := []string{"/", "/changelog", "/api/openapi.yaml", "/api/docs"}

	for _, path := range entries {
		if rec := env.request("anonymous", http.MethodGet, path); denied(rec) {
			t.Errorf("%s was refused for an anonymous visitor although areas are public", path)
		}
	}

	env.setAll(smmodel.LevelLoggedIn)

	for _, path := range entries {
		if rec := env.request("anonymous", http.MethodGet, path); !denied(rec) || rec.Header().Get("Location") != "/login" {
			t.Errorf("%s was served to an anonymous visitor on a closed server (%d)", path, rec.Code)
		}

		if rec := env.request("read", http.MethodGet, path); denied(rec) {
			t.Errorf("%s was refused for a reader on a closed server", path)
		}
	}
}

// the event popup shows data of the custom races, championships or race weekends and needs that area
func TestRouterEventDetailsFollowTheShownArea(t *testing.T) {
	env := newAccessTestEnv(t)
	defer env.cleanup()

	env.setAll(smmodel.LevelPublic)

	for query, area := range map[string]smmodel.Area{
		"custom-race=x":    smmodel.AreaCustomRaces,
		"championshipID=x": smmodel.AreaChampionships,
		"raceWeekendID=x":  smmodel.AreaRaceWeekends,
	} {
		env.setLevel(area, smmodel.LevelWrite)

		if rec := env.request("anonymous", http.MethodGet, "/event-details?"+query); rec.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403 for an anonymous visitor, got %d", query, rec.Code)
		}

		if rec := env.request("read", http.MethodGet, "/event-details?"+query); rec.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403 for a reader, got %d", query, rec.Code)
		}

		// the item does not exist: not found shows that the area was passed
		if rec := env.request("write", http.MethodGet, "/event-details?"+query); rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404 for a writer, got %d", query, rec.Code)
		}

		env.setLevel(area, smmodel.LevelPublic)
	}
}
