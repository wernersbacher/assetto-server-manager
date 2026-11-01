package servermanager

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// carClassPagesEnv is a router with real templates, the car class manager and one account per group.
type carClassPagesEnv struct {
	*teamsTestEnv

	router                                   http.Handler
	reader, writer, deleter                  *smmodel.Account
	usedClass, freeClass                     *smmodel.CarClass
	championship                             *smmodel.Championship
	listPath, usedPath, freePath, deletePath string
}

func newCarClassPagesEnv(t *testing.T) (*carClassPagesEnv, func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "car-class-pages")

	if err != nil {
		t.Fatal(err)
	}

	e, cleanup := newTeamsRenderEnv(t)

	e.resolver.resolveAccountManager().Init(teamsTestServerID)

	env := &carClassPagesEnv{teamsTestEnv: e, router: e.resolver.ResolveRouter(http.Dir(dir))}
	env.reader, _ = e.account(smmodel.GroupRead)
	env.writer, _ = e.account(smmodel.GroupWrite)
	env.deleter, _ = e.account(smmodel.GroupDelete)

	manager := e.resolver.resolveCarClassManager()

	if env.usedClass, err = manager.Create("GT3", []string{"ks_audi_r8_lms", "ks_bmw_m6_gt3"}); err != nil {
		t.Fatal(err)
	}

	if env.freeClass, err = manager.Create("Free <Class>", []string{"ks_mazda_mx5_cup"}); err != nil {
		t.Fatal(err)
	}

	env.championship = smmodel.NewChampionship("Used Cup")
	class := smmodel.NewChampionshipClass("Top")
	class.CarClassIDs = []uuid.UUID{env.usedClass.ID}
	env.championship.AddClass(class)

	if err := e.resolver.ResolveStore().UpsertChampionship(env.championship); err != nil {
		t.Fatal(err)
	}

	env.listPath = "/car-classes"
	env.usedPath = "/car-classes/" + env.usedClass.ID.String()
	env.freePath = "/car-classes/" + env.freeClass.ID.String()

	return env, func() {
		cleanup()
		_ = os.RemoveAll(dir)
	}
}

func (e *carClassPagesEnv) classNames() string {
	e.t.Helper()

	classes, err := e.resolver.resolveCarClassManager().List()

	if err != nil {
		e.t.Fatal(err)
	}

	var names []string

	for _, class := range classes {
		names = append(names, class.Name)
	}

	return strings.Join(names, "|")
}

func TestCarClassPagesRender(t *testing.T) {
	e, cleanup := newCarClassPagesEnv(t)
	defer cleanup()

	// the list is for everybody who can read, only writers get the link to the form
	expectBody(t, "list as reader", e.get(e.router, e.listPath, e.reader), http.StatusOK,
		[]string{"GT3", "Free &lt;Class&gt;", "1 championship", "Not used", e.usedPath},
		[]string{"/car-classes/new"})

	expectBody(t, "list as writer", e.get(e.router, e.listPath, e.writer), http.StatusOK,
		[]string{"/car-classes/new", "GT3"}, nil)

	// the form of a used class names the championships, shows the cars (also the ones that are not installed)
	// as selected and disables the delete button
	rec := e.get(e.router, e.usedPath, e.deleter)

	expectBody(t, "used class as deleter", rec, http.StatusOK,
		[]string{"Used Cup", "/championship/" + e.championship.ID.String(), "class Top", `value="ks_audi_r8_lms"`, `value="ks_bmw_m6_gt3"`,
			`selected="selected"`, `value="GT3"`, "cannot be deleted while a championship uses it"},
		[]string{"/car-classes/" + e.usedClass.ID.String() + "/delete"})

	if !strings.Contains(rec.Body.String(), "disabled") {
		t.Error("the delete button of a used class must be disabled")
	}

	// an unused class can be deleted
	expectBody(t, "free class as deleter", e.get(e.router, e.freePath, e.deleter), http.StatusOK,
		[]string{"No championship uses this car class", "/car-classes/" + e.freeClass.ID.String() + "/delete"}, nil)

	// writers can change, but not delete
	expectBody(t, "free class as writer", e.get(e.router, e.freePath, e.writer), http.StatusOK,
		[]string{`name="Name"`, `value="Free &lt;Class&gt;"`}, []string{"/delete"})

	// readers only see the cars
	expectBody(t, "free class as reader", e.get(e.router, e.freePath, e.reader), http.StatusOK,
		[]string{smcontent.PrettifyName("ks_mazda_mx5_cup", true)}, []string{`name="Name"`, "/delete"})

	expectBody(t, "new form", e.get(e.router, "/car-classes/new", e.writer), http.StatusOK,
		[]string{`action="/car-classes/new"`, `name="Cars"`}, nil)
}

func TestCarClassRoutesNeedThePermissionOfTheirGroup(t *testing.T) {
	e, cleanup := newCarClassPagesEnv(t)
	defer cleanup()

	before := e.classNames()

	// readers and visitors can neither open the form nor change anything
	for who, account := range map[string]*smmodel.Account{"anonymous": nil, "reader": e.reader} {
		if rec := e.get(e.router, "/car-classes/new", account); rec.Code == http.StatusOK {
			t.Errorf("%s: the new form must not open", who)
		}

		for _, target := range []string{"/car-classes/new", e.freePath} {
			rec := e.post(e.router, target, account, url.Values{"Name": {"Hacked"}, "Cars": {"x"}})

			if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
				t.Errorf("%s: POST %s answered %d", who, target, rec.Code)
			}
		}
	}

	// writers cannot delete
	rec := e.post(e.router, e.freePath+"/delete", e.writer, nil)

	if rec.Code == http.StatusFound && rec.Header().Get("Location") == "/car-classes" {
		t.Error("a writer deleted a car class")
	}

	if got := e.classNames(); got != before {
		t.Fatalf("the classes changed without permission: %q -> %q", before, got)
	}

	// writers create and change
	rec = e.post(e.router, "/car-classes/new", e.writer, url.Values{"Name": {"LMP"}, "Cars": {"ks_a", "ks_b"}})

	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/car-classes/") {
		t.Fatalf("create: expected a redirect to the class, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	rec = e.post(e.router, e.freePath, e.writer, url.Values{"Name": {"Free renamed"}, "Cars": {"ks_mazda_mx5_cup", "ks_x"}})

	if rec.Code != http.StatusFound {
		t.Fatalf("update: expected a redirect, got %d: %.300s", rec.Code, rec.Body.String())
	}

	if got := e.classNames(); got != "Free renamed|GT3|LMP" {
		t.Errorf("classes after create and rename: %q", got)
	}

	// deleters delete
	rec = e.post(e.router, e.freePath+"/delete", e.deleter, nil)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/car-classes" {
		t.Errorf("delete: expected a redirect to the list, got %d %q", rec.Code, rec.Header().Get("Location"))
	}

	if got := e.classNames(); got != "GT3|LMP" {
		t.Errorf("classes after delete: %q", got)
	}
}

func TestCarClassPagesShowRefusalsInsteadOfFailing(t *testing.T) {
	e, cleanup := newCarClassPagesEnv(t)
	defer cleanup()

	// unknown and invalid ids are 404
	for _, id := range []string{uuid.New().String(), "not-a-uuid"} {
		expectBody(t, "view "+id, e.get(e.router, "/car-classes/"+id, e.writer), http.StatusNotFound, nil, nil)
		expectBody(t, "update "+id, e.post(e.router, "/car-classes/"+id, e.writer, url.Values{"Name": {"X"}, "Cars": {"a"}}), http.StatusNotFound, nil, nil)
		expectBody(t, "delete "+id, e.post(e.router, "/car-classes/"+id+"/delete", e.deleter, nil), http.StatusNotFound, nil, nil)
	}

	// invalid input comes back as the form with the reason and the input, not as a server error
	rec := e.post(e.router, "/car-classes/new", e.writer, url.Values{"Name": {"gt3"}, "Cars": {"ks_x"}})

	expectBody(t, "name taken", rec, http.StatusUnprocessableEntity,
		[]string{"alert-danger", "There is already a car class with this name", `value="gt3"`, `value="ks_x"`}, nil)

	rec = e.post(e.router, "/car-classes/new", e.writer, url.Values{"Name": {"Empty"}})

	expectBody(t, "no cars", rec, http.StatusUnprocessableEntity, []string{"Choose at least one car"}, nil)

	// removing a car from a class changes nothing for the championship as long as nobody uses the car; a second class
	// with a car of the used class is refused (a car must not be in two classes of a championship)
	other, err := e.resolver.resolveCarClassManager().Create("Other", []string{"ks_other"})

	if err != nil {
		t.Fatal(err)
	}

	second := smmodel.NewChampionshipClass("Second")
	second.CarClassIDs = []uuid.UUID{other.ID}
	e.championship.AddClass(second)

	if err := e.resolver.ResolveStore().UpsertChampionship(e.championship); err != nil {
		t.Fatal(err)
	}

	rec = e.post(e.router, "/car-classes/"+other.ID.String(), e.writer, url.Values{"Name": {"Other"}, "Cars": {"ks_other", "ks_audi_r8_lms"}})

	expectBody(t, "car in two classes", rec, http.StatusUnprocessableEntity,
		[]string{"alert-danger", "Used Cup", "ks_audi_r8_lms", `value="ks_other"`}, nil)

	found, err := e.resolver.resolveCarClassManager().Get(other.ID.String())

	if err != nil || len(found.Cars) != 1 {
		t.Errorf("the refused change was saved: %v %v", found, err)
	}

	// deleting a used class is refused, with the message as flash on the page of the class
	rec = e.post(e.router, e.usedPath+"/delete", e.deleter, nil)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != e.usedPath {
		t.Fatalf("delete of a used class: expected a redirect back to the class, got %d %q: %.300s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}

	if !strings.Contains(e.classNames(), "GT3") {
		t.Error("a used car class was deleted")
	}

	followUp := e.getWithCookies(e.router, e.usedPath, e.deleter, rec.Result().Cookies())

	expectBody(t, "flash after the refused delete", followUp, http.StatusOK, []string{"is used by", "Used Cup"}, nil)
}

// getWithCookies is get with more cookies (the flash messages of a previous response).
func (e *carClassPagesEnv) getWithCookies(router http.Handler, target string, account *smmodel.Account, cookies []*http.Cookie) *httptest.ResponseRecorder {
	e.t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)

	for _, cookie := range e.loginCookies(account) {
		req.AddCookie(cookie)
	}

	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

func TestCarClassAPIAndLeaderboardFilter(t *testing.T) {
	e, cleanup := newCarClassPagesEnv(t)
	defer cleanup()

	// a closed server sends visitors to the login like every other API endpoint
	for _, target := range []string{"/api/car-classes", "/api/car-classes/" + e.usedClass.ID.String()} {
		if rec := e.get(e.router, target, nil); rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "login") {
			t.Errorf("anonymous GET %s: got %d %q", target, rec.Code, rec.Header().Get("Location"))
		}
	}

	rec := e.get(e.router, "/api/car-classes", e.reader)

	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}

	var classes []*api.CarClass

	if err := json.Unmarshal(rec.Body.Bytes(), &classes); err != nil {
		t.Fatal(err)
	}

	// sorted by name: "Free <Class>" before "GT3"
	if len(classes) != 2 || classes[0].Name != "Free <Class>" || classes[1].Name != "GT3" {
		t.Fatalf("classes: %+v", classes)
	}

	if classes[1].ID != e.usedClass.ID || len(classes[1].Cars) != 2 || classes[1].Cars[0].ID != "ks_audi_r8_lms" || classes[1].Cars[0].Name == "" {
		t.Errorf("GT3: %+v", classes[1])
	}

	rec = e.get(e.router, "/api/car-classes/"+e.freeClass.ID.String(), e.reader)

	var one api.CarClass

	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &one) != nil || one.ID != e.freeClass.ID || len(one.Cars) != 1 {
		t.Errorf("single: %d %s", rec.Code, rec.Body.String())
	}

	for _, id := range []string{uuid.New().String(), "not-a-uuid"} {
		if rec := e.get(e.router, "/api/car-classes/"+id, e.reader); rec.Code != http.StatusNotFound {
			t.Errorf("class %s: status %d, want 404", id, rec.Code)
		}
	}

	// the leaderboard knows the classes: a known one is fine (no laps, so an empty list), an unknown one is a 400
	for _, base := range []string{"/api/leaderboard?track=suzuka&carClass=", "/leaderboards?carClass="} {
		if rec := e.get(e.router, base+e.usedClass.ID.String(), e.reader); rec.Code != http.StatusOK {
			t.Errorf("%s known: status %d: %.200s", base, rec.Code, rec.Body.String())
		}

		for _, id := range []string{uuid.New().String(), "not-a-uuid"} {
			if rec := e.get(e.router, base+id, e.reader); rec.Code != http.StatusBadRequest {
				t.Errorf("%s%s: status %d, want 400", base, id, rec.Code)
			}
		}
	}

	// the page renders (without laps it shows the empty state, the filter form is checked by the smoke test)
	expectBody(t, "page", e.get(e.router, "/leaderboards", e.reader), http.StatusOK, nil, nil)
}
