package admin

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
)

func tempInstall(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := ioutil.TempDir("", "currentconfig")

	if err != nil {
		t.Fatal(err)
	}

	return dir, func() { os.RemoveAll(dir) }
}

func TestReadCurrentConfigMissingFiles(t *testing.T) {
	dir, cleanup := tempInstall(t)
	defer cleanup()

	vars := readCurrentConfig(dir)

	if !vars.NotWritten || vars.ReadError != "" || vars.ConfigText != "" || vars.EntryListText != "" {
		t.Errorf("expected the not written state, got %+v", vars)
	}
}

func TestReadCurrentConfigPresentFiles(t *testing.T) {
	dir, cleanup := tempInstall(t)
	defer cleanup()

	cfg := filepath.Join(dir, serverenv.ConfigPath)

	if err := os.MkdirAll(cfg, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(cfg, "server_cfg.ini"), []byte("[SERVER]\nNAME=x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ioutil.WriteFile(filepath.Join(cfg, "entry_list.ini"), []byte("[CAR_0]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	vars := readCurrentConfig(dir)

	if vars.NotWritten || vars.ReadError != "" || vars.ConfigText != "[SERVER]\nNAME=x\n" || vars.EntryListText != "[CAR_0]\n" {
		t.Errorf("unexpected vars %+v", vars)
	}
}

func TestReadCurrentConfigReadError(t *testing.T) {
	dir, cleanup := tempInstall(t)
	defer cleanup()

	// a directory in place of the file cannot be read, on every OS
	if err := os.MkdirAll(filepath.Join(dir, serverenv.ConfigPath, "server_cfg.ini"), 0755); err != nil {
		t.Fatal(err)
	}

	vars := readCurrentConfig(dir)

	if vars.ReadError == "" {
		t.Errorf("expected a read error, got %+v", vars)
	}
}
