package contenthttp

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/downloads"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
	"github.com/wernersbacher/ACPitwall/internal/skins"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

func account(group smmodel.Group) *smmodel.Account {
	return &smmodel.Account{ID: uuid.New(), Name: "acc-" + string(group), Groups: map[smmodel.ServerID]smmodel.Group{"s": group}}
}

func TestUserSkinsAreOnlyServedToWhoCanUseThem(t *testing.T) {
	store, cleanupStore := envtest.TempJSONStore()
	defer cleanupStore()

	install, err := ioutil.TempDir("", "user-skin-download")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(install)

	if err := os.MkdirAll(filepath.Join(install, "content", "cars", "dev_car", "skins"), 0755); err != nil {
		t.Fatal(err)
	}

	teamID := uuid.New()
	owner, teamMate, stranger, admin := account(smmodel.GroupRead), account(smmodel.GroupRead), account(smmodel.GroupRead), account(smmodel.GroupAdmin)
	members := map[uuid.UUID][]uuid.UUID{teamMate.ID: {teamID}, owner.ID: {teamID}}

	manager := skins.NewManager(store, func() string { return install },
		func() smmodel.SkinSettings { return smmodel.SkinSettings{UserUploads: true} },
		func(a uuid.UUID) []uuid.UUID { return members[a] })

	file := func(name string) skins.File {
		return skins.File{Path: "mine/" + name, Size: 3, Open: func() (io.ReadCloser, error) { return ioutil.NopCloser(bytes.NewReader([]byte("abc"))), nil }}
	}

	uploaded, err := manager.Upload(skins.Actor{ID: owner.ID}, "dev_car", smmodel.SkinPrivate, uuid.Nil, []skins.File{file("ui_skin.json"), file("preview.jpg")})

	if err != nil {
		t.Fatal(err)
	}

	folder := uploaded[0].Folder

	h := &CarsHandler{env: envtest.New(install)}
	h.SetUserSkins(manager)

	request := func(handler http.HandlerFunc, acc *smmodel.Account, query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/car/x/skin/y/download?"+query, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("car_id", "dev_car")
		rctx.URLParams.Add("skin", folder)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = smweb.WithScope(req, &smweb.Scope{ServerID: "s"})

		if acc != nil {
			req = smweb.WithAccount(req, acc)
		}

		rec := httptest.NewRecorder()
		handler(rec, req)

		return rec
	}

	for name, handler := range map[string]http.HandlerFunc{"download": h.DownloadSkin, "preview": h.SkinPreview} {
		if code := request(handler, owner, "").Code; code != http.StatusOK {
			t.Errorf("%s: the owner must get their private skin, got %d", name, code)
		}

		for who, acc := range map[string]*smmodel.Account{"anonymous": nil, "stranger": stranger, "team mate of a private skin's owner": teamMate} {
			if code := request(handler, acc, "").Code; code != http.StatusNotFound {
				t.Errorf("%s: %s must not get a private skin, got %d", name, who, code)
			}
		}

		if code := request(handler, admin, "").Code; code != http.StatusOK {
			t.Errorf("%s: an admin sees every skin, got %d", name, code)
		}
	}

	// shared with the team: the team mate gets it, the stranger still not
	if err := manager.SetVisibility(skins.Actor{ID: owner.ID}, uploaded[0].ID.String(), smmodel.SkinTeam, teamID); err != nil {
		t.Fatal(err)
	}

	if code := request(h.DownloadSkin, teamMate, "").Code; code != http.StatusOK {
		t.Errorf("a team mate must get a team skin, got %d", code)
	}

	if code := request(h.DownloadSkin, stranger, "").Code; code != http.StatusNotFound {
		t.Errorf("a stranger must not get a team skin, got %d", code)
	}

	// a signed URL (handed out for the entry list) is enough, a wrong signature is not
	signer := downloads.NewSignerWithSecret([]byte("0123456789abcdef0123456789abcdef"))
	h.signer = signer

	q, err := signer.SkinQuery("dev_car", folder, time.Now().Add(time.Hour))

	if err != nil {
		t.Fatal(err)
	}

	if code := request(h.DownloadSkin, nil, q.Encode()).Code; code != http.StatusOK {
		t.Errorf("a signed URL must work without a login, got %d", code)
	}

	q.Set("sig", "00"+q.Get("sig")[2:])

	if code := request(h.DownloadSkin, nil, q.Encode()).Code; code != http.StatusNotFound {
		t.Errorf("a wrong signature must not work, got %d", code)
	}

	// the preview is never served on a signature
	q, _ = signer.SkinQuery("dev_car", folder, time.Now().Add(time.Hour))

	if code := request(h.SkinPreview, nil, q.Encode()).Code; code != http.StatusNotFound {
		t.Errorf("the preview needs a login, got %d", code)
	}
}
