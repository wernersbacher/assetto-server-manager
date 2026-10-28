package servermanager

import (
	"bytes"
	"context"
	"github.com/cj123/sessions"
	"github.com/go-chi/chi"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkinUploadFilePath(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{filename: "my_skin/livery.png", expected: "my_skin/livery.png"},
		{filename: "my_skin/sub/preview.jpg", expected: "my_skin/sub/preview.jpg"},
		{filename: `my_skin\ui_skin.json`, expected: "my_skin/ui_skin.json"},
		{filename: "livery.png", expected: "livery.png"},
		{filename: "../../../evil/livery.png", expected: "evil/livery.png"},
		{filename: "my_skin/../../livery.png", expected: "livery.png"},
		{filename: "/etc/livery.png", expected: "etc/livery.png"},
	}

	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
			// encode and parse a real multipart form, as the browser sends it
			body := new(bytes.Buffer)
			mw := multipart.NewWriter(body)

			fw, err := mw.CreateFormFile("input-folder-skin[]", test.filename)

			if err != nil {
				t.Fatal(err)
			}

			if _, err := fw.Write([]byte("data")); err != nil {
				t.Fatal(err)
			}

			if err := mw.Close(); err != nil {
				t.Fatal(err)
			}

			form, err := multipart.NewReader(body, mw.Boundary()).ReadForm(1 << 20)

			if err != nil {
				t.Fatal(err)
			}

			out := skinUploadFilePath(form.File["input-folder-skin[]"][0])

			if out != filepath.FromSlash(test.expected) {
				t.Errorf("expected %q, got %q", filepath.FromSlash(test.expected), out)
			}
		})
	}
}

func TestCarsHandlerUploadSkin(t *testing.T) {
	oldInstallPath := ServerInstallPath
	defer func() { ServerInstallPath = oldInstallPath }()

	oldSessionsStore := sessionsStore
	sessionsStore = sessions.NewCookieStore([]byte("test"))
	defer func() { sessionsStore = oldSessionsStore }()

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
		req.Header.Set("Content-Type", mw.FormDataContentType())

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "dev_car")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		rec := httptest.NewRecorder()
		(&CarsHandler{carManager: &CarManager{}}).uploadSkin(rec, req)

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
