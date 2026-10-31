package contenthttp

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi"

	"github.com/wernersbacher/ACPitwall/internal/downloads"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
)

func TestCarsHandlerDownloadSkin(t *testing.T) {
	root, err := ioutil.TempDir("", "skin-download")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	installPath := filepath.Join(root, "install")
	skinsDir := filepath.Join(installPath, "content", "cars", "dev_car", "skins")

	for p, content := range map[string]string{
		filepath.Join(skinsDir, "my_skin", "ui_skin.json"):                   "{}",
		filepath.Join(skinsDir, "my_skin", "sub", "body.dds"):                "dds",
		filepath.Join(skinsDir, "other_skin", "ui_skin.json"):                "other",
		filepath.Join(installPath, "server_cfg_secret.ini"):                  "secret",
		filepath.Join(installPath, "content", "cars", "dev_car", "data.acd"): "acd",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	download := func(car, skin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/car/x/skin/y/download", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("car_id", car)
		rctx.URLParams.Add("skin", skin)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		(&CarsHandler{env: envtest.New(installPath)}).DownloadSkin(rec, req)

		return rec
	}

	rec := download("dev_car", "my_skin")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))

	if err != nil {
		t.Fatal(err)
	}

	var names []string

	for _, f := range zr.File {
		names = append(names, f.Name)
	}

	sort.Strings(names)

	want := "content/cars/dev_car/skins/my_skin/sub/body.dds,content/cars/dev_car/skins/my_skin/ui_skin.json"

	if strings.Join(names, ",") != want {
		t.Errorf("zip entries %v, want %s", names, want)
	}

	for _, c := range [][2]string{
		{"dev_car", "missing"},
		{"dev_car", ".."},
		{"dev_car", "../.."},
		{"..", "my_skin"},
		{"dev_car", "my_skin/.."},
		{"dev_car", `..\..`},
		{"dev_car", ""},
	} {
		if code := download(c[0], c[1]).Code; code != http.StatusNotFound {
			t.Errorf("%v: status %d, want 404", c, code)
		}
	}
}

func skinRequest(h *CarsHandler, car, skin, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/car/x/skin/y/download?"+query, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("car_id", car)
	rctx.URLParams.Add("skin", skin)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.DownloadSkin(rec, req)

	return rec
}

func TestCarsHandlerDownloadSkinLimits(t *testing.T) {
	root, err := ioutil.TempDir("", "skin-limits")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	skinsDir := filepath.Join(root, "content", "cars", "c", "skins")

	if err := os.MkdirAll(filepath.Join(skinsDir, "many"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(skinsDir, "ok"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(skinsDir, "ok", "a.dds"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	for i := 0; i <= MaxSkinZipFiles; i++ {
		if err := ioutil.WriteFile(filepath.Join(skinsDir, "many", fmt.Sprintf("f%d", i)), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	h := &CarsHandler{env: envtest.New(root), zipSlots: make(chan struct{}, 1)}

	if code := skinRequest(h, "c", "many", "").Code; code != http.StatusRequestEntityTooLarge {
		t.Errorf("too many files: status %d, want 413", code)
	}

	if code := skinRequest(h, "c", "ok", "").Code; code != http.StatusOK {
		t.Errorf("small skin: status %d", code)
	}

	// all slots busy
	h.zipSlots <- struct{}{}

	if rec := skinRequest(h, "c", "ok", ""); rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("busy: status %d, want 503 with Retry-After", rec.Code)
	}
}

func TestCarsHandlerValidSkinSignature(t *testing.T) {
	signer := downloads.NewSignerWithSecret([]byte("0123456789abcdef0123456789abcdef"))
	h := &CarsHandler{signer: signer}

	request := func(car, skin, query string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/car/x/skin/y/download?"+query, nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("car_id", car)
		rctx.URLParams.Add("skin", skin)

		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	q, err := signer.SkinQuery("c", "s", time.Now().Add(time.Hour))

	if err != nil {
		t.Fatal(err)
	}

	if !h.ValidSkinSignature(request("c", "s", q.Encode())) {
		t.Error("valid signature rejected")
	}

	if h.ValidSkinSignature(request("c", "other", q.Encode())) || h.ValidSkinSignature(request("c", "s", "")) {
		t.Error("signature for another skin or no signature accepted")
	}

	expired, _ := signer.SkinQuery("c", "s", time.Now().Add(-time.Minute))

	if h.ValidSkinSignature(request("c", "s", expired.Encode())) {
		t.Error("expired signature accepted")
	}

	if (&CarsHandler{}).ValidSkinSignature(request("c", "s", q.Encode())) {
		t.Error("handler without signer accepted a signature")
	}
}
