package skins

import (
	"bytes"
	"mime/multipart"
	"testing"
)

// TestFilesFromMultipart encodes and parses a real multipart form, as the browser sends it, and checks which
// skin and file name come out.
func TestFilesFromMultipart(t *testing.T) {
	tests := []struct {
		filename string
		skin     string // "" if the upload must be refused
		file     string
	}{
		{filename: "my_skin/livery.png", skin: "my_skin", file: "livery.png"},
		{filename: "my_skin/sub/preview.jpg", skin: "my_skin", file: "sub/preview.jpg"},
		{filename: `my_skin\ui_skin.json`, skin: "my_skin", file: "ui_skin.json"},
		{filename: "livery.png"},
		{filename: "../../../evil/livery.png", skin: "evil", file: "livery.png"},
		{filename: "my_skin/../../other/livery.png", skin: "other", file: "livery.png"},
		{filename: "/etc/livery.png", skin: "etc", file: "livery.png"},
		{filename: "../livery.png"},
	}

	for _, test := range tests {
		t.Run(test.filename, func(t *testing.T) {
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

			files := FilesFromMultipart(form.File)

			if len(files) != 1 || files[0].Size != 4 {
				t.Fatalf("unexpected files: %+v", files)
			}

			groups, err := groupFiles(files)

			if test.skin == "" {
				if err == nil {
					t.Fatalf("expected the upload to be refused, got %+v", groups[0])
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if groups[0].name != test.skin || groups[0].files[0].Path != test.file {
				t.Errorf("expected %s/%s, got %s/%s", test.skin, test.file, groups[0].name, groups[0].files[0].Path)
			}
		})
	}
}
