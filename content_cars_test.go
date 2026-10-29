package servermanager

import (
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	"github.com/wernersbacher/assetto-server-manager/internal/plainname"

	"bytes"
	"context"
	"github.com/go-chi/chi"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCarsHandlerUploadSkin(t *testing.T) {
	oldInstallPath := ServerInstallPath
	defer func() { ServerInstallPath = oldInstallPath }()

	root, err := ioutil.TempDir("", "skin-upload")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	ServerInstallPath = filepath.Join(root, "install")
	skinsDir := filepath.Join(ServerInstallPath, "content", "cars", "dev_car", "skins")

	if err := os.MkdirAll(skinsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// a file that a traversal attempt would overwrite
	if err := ioutil.WriteFile(filepath.Join(ServerInstallPath, "content", "cars", "dev_car", "keep.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	files := []struct{ name, content string }{
		{"my_skin/livery.png", "livery"},
		{"my_skin/preview.jpg", "preview"},
		{"my_skin/sub/x.dds", "dds"},
		{`win_skin\ui_skin.json`, "{}"},
		{"../keep.txt", "overwritten"},
		{"../../../../escaped.txt", "escaped"},
		{"/abs_skin/livery.png", "abs"},
	}

	// upload twice: the second upload must overwrite the first one without errors
	for _, content := range []string{"first", "second"} {
		body := new(bytes.Buffer)
		mw := multipart.NewWriter(body)

		for _, f := range files {
			fw, err := mw.CreateFormFile("input-folder-skin[]", f.name)

			if err != nil {
				t.Fatal(err)
			}

			if _, err := fw.Write([]byte(content + f.content)); err != nil {
				t.Fatal(err)
			}
		}

		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodPost, "/car/dev_car/skin", body)
		req = withRequestScope(req, testRequestScope())
		req.Header.Set("Content-Type", mw.FormDataContentType())

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "dev_car")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		(&CarsHandler{carManager: smcontent.NewCarManager(ServerInstallPath, false, false)}).uploadSkin(rec, req)

		if rec.Code != http.StatusFound && rec.Code != http.StatusSeeOther {
			t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
		}
	}

	expected := map[string]string{
		"my_skin/livery.png":    "secondlivery",
		"my_skin/preview.jpg":   "secondpreview",
		"my_skin/sub/x.dds":     "seconddds",
		"win_skin/ui_skin.json": "second{}",
		"escaped.txt":           "secondescaped",
		"keep.txt":              "secondoverwritten",
		"abs_skin/livery.png":   "secondabs",
	}

	for name, content := range expected {
		b, err := ioutil.ReadFile(filepath.Join(skinsDir, filepath.FromSlash(name)))

		if err != nil {
			t.Errorf("expected %s in the skins directory: %v", name, err)
			continue
		}

		if string(b) != content {
			t.Errorf("%s: expected %q, got %q", name, content, string(b))
		}
	}

	// nothing may be written outside of the skins directory
	b, err := ioutil.ReadFile(filepath.Join(ServerInstallPath, "content", "cars", "dev_car", "keep.txt"))

	if err != nil || string(b) != "keep" {
		t.Errorf("file outside of the skins directory was modified: %q, %v", b, err)
	}

	err = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		rel, _ := filepath.Rel(skinsDir, p)

		if strings.HasPrefix(rel, "..") && filepath.Base(p) != "keep.txt" {
			t.Errorf("unexpected file outside of the skins directory: %s", p)
		}

		return nil
	})

	if err != nil {
		t.Fatal(err)
	}
}

func TestCarsHandlerDeleteSkin(t *testing.T) {
	oldInstallPath := ServerInstallPath
	defer func() { ServerInstallPath = oldInstallPath }()

	root, err := ioutil.TempDir("", "skin-delete")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	ServerInstallPath = filepath.Join(root, "install")
	carDir := filepath.Join(ServerInstallPath, "content", "cars", "dev_car")
	skinsDir := filepath.Join(carDir, "skins")

	for _, dir := range []string{
		filepath.Join(skinsDir, "my_skin"),
		filepath.Join(skinsDir, "other_skin"),
		filepath.Join(ServerInstallPath, "content", "tracks", "keep_track"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(dir, "file.txt"), []byte("keep"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// a plain file in the skins directory is not a skin
	if err := ioutil.WriteFile(filepath.Join(skinsDir, "plain_file"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	deleteSkin := func(car, skin string) int {
		form := url.Values{"skin-delete": {skin}}
		req := httptest.NewRequest(http.MethodPost, "/car/"+car+"/skin/delete", strings.NewReader(form.Encode()))
		req = withRequestScope(req, testRequestScope())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", car)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		(&CarsHandler{carManager: smcontent.NewCarManager(ServerInstallPath, false, false)}).deleteSkin(rec, req)

		return rec.Code
	}

	countFiles := func() (n int) {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				n++
			}

			return nil
		})

		return n
	}

	before := countFiles()

	for _, skin := range []string{"../..", "..", ".", "a/b", `a\b`, "../skins/my_skin", "", "does_not_exist", "plain_file", "../../../tracks"} {
		if code := deleteSkin("dev_car", skin); code != http.StatusBadRequest && code != http.StatusNotFound {
			t.Errorf("skin %q: expected 400 or 404, got %d", skin, code)
		}

		if n := countFiles(); n != before {
			t.Fatalf("skin %q: deleted files (%d before, %d after)", skin, before, n)
		}
	}

	// invalid or unknown car names must not delete anything either
	for _, car := range []string{"..", "does_not_exist"} {
		if code := deleteSkin(car, "my_skin"); code != http.StatusBadRequest && code != http.StatusNotFound {
			t.Errorf("car %q: expected 400 or 404, got %d", car, code)
		}
	}

	if n := countFiles(); n != before {
		t.Fatalf("deleted files for invalid car (%d before, %d after)", before, n)
	}

	if code := deleteSkin("dev_car", "my_skin"); code != http.StatusFound && code != http.StatusSeeOther {
		t.Fatalf("valid skin: unexpected status %d", code)
	}

	if _, err := os.Stat(filepath.Join(skinsDir, "my_skin")); !os.IsNotExist(err) {
		t.Errorf("valid skin was not deleted: %v", err)
	}

	if _, err := os.Stat(filepath.Join(skinsDir, "other_skin", "file.txt")); err != nil {
		t.Errorf("other skin was deleted: %v", err)
	}
}

func TestValidPlainName(t *testing.T) {
	for _, name := range []string{"dev_car", "my skin", "setup.ini", "..a", "a..", "v1.0"} {
		if !plainname.Valid(name) {
			t.Errorf("%q should be valid", name)
		}
	}

	for _, name := range []string{"", ".", "..", "../..", "a/b", `a\b`, "/a", `\a`, "a/", "C:", "a\x00b"} {
		if plainname.Valid(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

func TestValidContentFilePath(t *testing.T) {
	for _, p := range []string{"car/ui/ui_car.json", "car/skins/a/preview.jpg", `car\data\x.ini`, "file"} {
		if !validContentFilePath(p) {
			t.Errorf("%q should be valid", p)
		}
	}

	for _, p := range []string{"", "..", "../x", "a/../../x", "a/./b", "/etc/x", "a//b", `a\..\..\x`, "a/b/"} {
		if validContentFilePath(p) {
			t.Errorf("%q should be invalid", p)
		}
	}
}

func TestContentUploadRejectsPathTraversal(t *testing.T) {
	oldInstallPath := ServerInstallPath
	defer func() { ServerInstallPath = oldInstallPath }()

	root, err := ioutil.TempDir("", "content-upload")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	ServerInstallPath = filepath.Join(root, "install")

	cuh := &ContentUploadHandler{}
	err = cuh.addFiles([]ContentFile{{Name: "x", FilePath: "../../../escaped.txt", Data: "", Size: 0}}, ContentTypeWeather)

	if err == nil {
		t.Error("expected an error for a traversing file path")
	}

	if _, err := os.Stat(filepath.Join(root, "escaped.txt")); !os.IsNotExist(err) {
		t.Errorf("file was written outside of the content directory: %v", err)
	}
}

func TestCarSetupDeleteHandlerRejectsPathTraversal(t *testing.T) {
	oldInstallPath := ServerInstallPath
	defer func() { ServerInstallPath = oldInstallPath }()

	root, err := ioutil.TempDir("", "setup-delete")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	ServerInstallPath = filepath.Join(root, "install")
	setupDir := filepath.Join(ServerInstallPath, "setups", "car", "track")

	if err := os.MkdirAll(setupDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(setupDir, "a.ini"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	for _, params := range [][3]string{{"..", "..", ".."}, {"car", "..", ".."}, {"car", "track", ".."}, {`car\..`, "track", "a.ini"}} {
		req := httptest.NewRequest(http.MethodGet, "/setups/delete/x/y/z", nil)
		req = withRequestScope(req, testRequestScope())
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("car", params[0])
		rctx.URLParams.Add("track", params[1])
		rctx.URLParams.Add("setup", params[2])
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		carSetupDeleteHandler(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%v: expected 404, got %d", params, rec.Code)
		}

		if _, err := os.Stat(filepath.Join(setupDir, "a.ini")); err != nil {
			t.Fatalf("%v: setup file was deleted: %v", params, err)
		}
	}
}
