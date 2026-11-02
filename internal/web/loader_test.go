package web

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesystemTemplateLoaderFingerprint(t *testing.T) {
	dir, err := ioutil.TempDir("", "views")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	file := filepath.Join(dir, "pages", "a.html")

	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(file, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewFilesystemTemplateLoader(dir).(ChangeDetector)

	first, err := loader.Fingerprint()

	if err != nil {
		t.Fatal(err)
	}

	if again, _ := loader.Fingerprint(); again != first {
		t.Errorf("fingerprint of unchanged files changed: %s -> %s", first, again)
	}

	// same size, later modification time
	later := time.Now().Add(time.Hour)

	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}

	touched, _ := loader.Fingerprint()

	if touched == first {
		t.Error("fingerprint did not change when a file was modified")
	}

	if err := ioutil.WriteFile(filepath.Join(dir, "pages", "b.html"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}

	if added, _ := loader.Fingerprint(); added == touched {
		t.Error("fingerprint did not change when a file was added")
	}
}
