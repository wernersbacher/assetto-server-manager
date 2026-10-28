package servermanager

import (
	"bytes"
	"mime/multipart"
	"path/filepath"
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
