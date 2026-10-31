package contenthttp

import (
	"bytes"
	"context"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	"github.com/wernersbacher/ACPitwall/internal/skins"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

type skinUploadEnv struct {
	t        *testing.T
	install  string
	settings smmodel.SkinSettings
	teams    map[uuid.UUID][]uuid.UUID
	manager  *skins.Manager
	handler  *CarsHandler
	cleanup  func()
}

func newSkinUploadEnv(t *testing.T) *skinUploadEnv {
	t.Helper()

	store, cleanupStore := envtest.TempJSONStore()

	install, err := ioutil.TempDir("", "skin-upload")

	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(install, "content", "cars", "dev_car", "skins"), 0755); err != nil {
		t.Fatal(err)
	}

	e := &skinUploadEnv{t: t, install: install, teams: map[uuid.UUID][]uuid.UUID{}, cleanup: func() { cleanupStore(); _ = os.RemoveAll(install) }}

	e.manager = skins.NewManager(store, func() string { return install },
		func() smmodel.SkinSettings { return e.settings },
		func(a uuid.UUID) []uuid.UUID { return e.teams[a] })

	e.handler = &CarsHandler{env: envtest.New(install)}
	e.handler.SetUserSkins(e.manager)

	return e
}

// upload posts the files (name -> content) and the form values as the account (nil: anonymous).
func (e *skinUploadEnv) upload(handler http.HandlerFunc, acc *smmodel.Account, files map[string]string, values url.Values) *httptest.ResponseRecorder {
	e.t.Helper()

	body := new(bytes.Buffer)
	mw := multipart.NewWriter(body)

	for name, content := range files {
		fw, err := mw.CreateFormFile("input-folder-skin[]", name)

		if err != nil {
			e.t.Fatal(err)
		}

		_, _ = fw.Write([]byte(content))
	}

	for key, vs := range values {
		for _, v := range vs {
			_ = mw.WriteField(key, v)
		}
	}

	if err := mw.Close(); err != nil {
		e.t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/car/dev_car/skin", body)
	req = smweb.WithScope(req, webtest.RequestScope())
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "dev_car")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	if acc != nil {
		req = smweb.WithAccount(req, acc)
	}

	rec := httptest.NewRecorder()
	handler(rec, req)

	return rec
}

func (e *skinUploadEnv) records() []*smmodel.UserSkin {
	e.t.Helper()

	list, err := e.manager.ListForCar("dev_car", skins.Actor{ID: uuid.New(), Writer: true})

	if err != nil {
		e.t.Fatal(err)
	}

	return list
}

func TestUploadSkinAsAdminIsGlobal(t *testing.T) {
	e := newSkinUploadEnv(t)
	defer e.cleanup()

	admin := account(smmodel.GroupAdmin)
	files := map[string]string{"my_skin/livery.png": "livery", "my_skin/preview.jpg": "preview", "my_skin/ui_skin.json": "{}"}

	// twice: the second upload replaces the first
	for _, content := range []string{"first", "second"} {
		for name := range files {
			files[name] = content + name
		}

		if rec := e.upload(e.handler.UploadSkin, admin, files, nil); rec.Code != http.StatusFound {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
	}

	b, err := ioutil.ReadFile(filepath.Join(e.install, "content", "cars", "dev_car", "skins", "my_skin", "livery.png"))

	if err != nil || string(b) != "secondmy_skin/livery.png" {
		t.Errorf("the admin upload belongs into the skins directory of the car: %q %v", b, err)
	}

	if list := e.records(); len(list) != 1 || list[0].Visibility != smmodel.SkinGlobal || list[0].Folder != "my_skin" || list[0].Owner != admin.ID {
		t.Errorf("unexpected records: %+v", list)
	}

	// a traversing name stays inside
	e.upload(e.handler.UploadSkin, admin, map[string]string{"../../../../escaped/livery.png": "x"}, nil)

	if _, err := os.Stat(filepath.Join(e.install, "escaped")); err == nil {
		t.Error("a file escaped the car directory")
	}
}

func TestUploadSkinByUsers(t *testing.T) {
	e := newSkinUploadEnv(t)
	defer e.cleanup()

	user, stranger := account(smmodel.GroupRead), account(smmodel.GroupRead)
	files := map[string]string{"blue/livery.png": "livery", "blue/ui_skin.json": "{}"}

	// switched off: nothing is stored, anonymous visitors cannot upload in any case
	for name, acc := range map[string]*smmodel.Account{"off": user, "anonymous": nil} {
		e.settings = smmodel.SkinSettings{UserUploads: name == "anonymous"}

		if rec := e.upload(e.handler.UploadSkin, acc, files, nil); rec.Code != http.StatusFound {
			t.Errorf("%s: expected a redirect with an error message, got %d", name, rec.Code)
		}

		if len(e.records()) != 0 {
			t.Fatalf("%s: nothing may be stored", name)
		}
	}

	e.settings = smmodel.SkinSettings{UserUploads: true}

	// users upload private by default and cannot ask for global
	if rec := e.upload(e.handler.UploadSkin, user, files, nil); rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}

	list := e.records()

	if len(list) != 1 || list[0].Visibility != smmodel.SkinPrivate || list[0].Owner != user.ID {
		t.Fatalf("unexpected records: %+v", list)
	}

	if _, err := os.Stat(filepath.Join(e.install, "content", "cars", "dev_car", "skins", list[0].Folder)); err == nil {
		t.Error("a private skin must not be in the skins directory of the car")
	}

	e.upload(e.handler.UploadSkin, user, map[string]string{"red/livery.png": "x"}, url.Values{"visibility": {"global"}})

	if len(e.records()) != 1 {
		t.Error("global upload must be refused while sharing is off")
	}

	e.upload(e.handler.UploadSkin, user, map[string]string{"red/livery.png": "x"}, url.Values{"visibility": {"bogus"}})

	if len(e.records()) != 1 {
		t.Error("an unknown visibility must be refused")
	}

	id := list[0].ID.String()
	change := func(acc *smmodel.Account, handler http.HandlerFunc, form url.Values) {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(form.Encode()))
		req = smweb.WithScope(req, webtest.RequestScope())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "dev_car")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		if acc != nil {
			req = smweb.WithAccount(req, acc)
		}

		handler(httptest.NewRecorder(), req)
	}

	// somebody else cannot change or delete it
	change(stranger, e.handler.SetSkinVisibility, url.Values{"skin": {id}, "visibility": {"private"}})
	change(stranger, e.handler.RemoveSkin, url.Values{"skin": {id}})
	change(nil, e.handler.RemoveSkin, url.Values{"skin": {id}})

	if len(e.records()) != 1 {
		t.Fatal("a stranger must not delete the skin")
	}

	// the owner shares with a team and deletes
	team := uuid.New()
	e.teams[user.ID] = []uuid.UUID{team}

	change(user, e.handler.SetSkinVisibility, url.Values{"skin": {id}, "visibility": {"team"}, "team": {team.String()}})

	if got := e.records(); got[0].Visibility != smmodel.SkinTeam || got[0].TeamID != team {
		t.Errorf("the skin must be shared with the team: %+v", got[0])
	}

	// ids from the outside never reach the file system
	change(user, e.handler.RemoveSkin, url.Values{"skin": {"../../../etc/passwd"}})
	change(user, e.handler.SetSkinVisibility, url.Values{"skin": {`..\..\x`}, "visibility": {"private"}})

	change(user, e.handler.RemoveSkin, url.Values{"skin": {id}})

	if len(e.records()) != 0 {
		t.Error("the owner must be able to delete the skin")
	}

	if _, err := os.Stat(filepath.Join(e.install, "content", "user-skins", "dev_car", list[0].Folder)); err == nil {
		t.Error("the files must be deleted")
	}
}

func TestUploadSkinLimitsAreAppliedToTheBody(t *testing.T) {
	e := newSkinUploadEnv(t)
	defer e.cleanup()

	e.settings = smmodel.SkinSettings{UserUploads: true, MaxSkinMB: 1, UserQuotaMB: 1}
	user := account(smmodel.GroupRead)

	big := strings.Repeat("x", 3<<20)

	if rec := e.upload(e.handler.UploadSkin, user, map[string]string{"big/livery.png": big}, nil); rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}

	if len(e.records()) != 0 {
		t.Error("a skin over the limit must not be stored")
	}

	// not enough free disk space: refused before the body is read
	e.settings = smmodel.SkinSettings{UserUploads: true, MinFreeMB: 1 << 30}

	e.upload(e.handler.UploadSkin, user, map[string]string{"small/livery.png": "x"}, nil)

	if len(e.records()) != 0 {
		t.Error("an upload must be refused when the disk reserve is not left")
	}
}
