package contenthttp

import (
	"archive/zip"
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi"

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
