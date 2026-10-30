package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readConfig(t *testing.T, content string) (*Configuration, error) {
	t.Helper()

	dir, err := ioutil.TempDir("", "config-test")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	location := filepath.Join(dir, "config.yml")

	if err := ioutil.WriteFile(location, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	return ReadConfig(location)
}

func TestReadConfigDefaultSlot(t *testing.T) {
	conf, err := readConfig(t, "steam:\n  install_path: assetto\n  executable_path: myServer\n")

	if err != nil {
		t.Fatal(err)
	}

	slots, err := conf.Slots()

	if err != nil {
		t.Fatal(err)
	}

	if len(slots) != 1 {
		t.Fatalf("expected 1 slot, got %d", len(slots))
	}

	wd, _ := os.Getwd()
	slot := slots[0]

	if !slot.IsDefault() || slot.Name != DefaultSlotName {
		t.Errorf("unexpected default slot: %+v", slot)
	}

	if slot.InstallPath != filepath.Join(wd, "assetto") {
		t.Errorf("install path not made absolute: %s", slot.InstallPath)
	}

	if slot.ExecutablePath != "myServer" {
		t.Errorf("unexpected executable path: %s", slot.ExecutablePath)
	}
}

func TestReadConfigServers(t *testing.T) {
	abs, _ := filepath.Abs("elsewhere")

	conf, err := readConfig(t, `
steam:
  install_path: ignored
  executable_path: acServerGlobal
servers:
  - id: a
    name: Alpha
    install_path: '`+abs+`'
    executable_path: acServerA
  - id: b
    install_path: relative
`)

	if err != nil {
		t.Fatal(err)
	}

	slots, err := conf.Slots()

	if err != nil {
		t.Fatal(err)
	}

	if len(slots) != 2 {
		t.Fatalf("expected 2 slots, got %d", len(slots))
	}

	if slots[0].ID != "a" || slots[0].Name != "Alpha" || slots[0].InstallPath != abs || slots[0].ExecutablePath != "acServerA" || slots[0].IsDefault() {
		t.Errorf("unexpected first slot: %+v", slots[0])
	}

	if slots[1].Name != "b" || slots[1].ExecutablePath != "acServerGlobal" || !filepath.IsAbs(slots[1].InstallPath) {
		t.Errorf("unexpected second slot: %+v", slots[1])
	}
}

func TestReadConfigServersInvalid(t *testing.T) {
	for name, tc := range map[string]struct {
		yaml, errContains string
	}{
		"empty id":       {"servers:\n  - install_path: a\n", "no id"},
		"duplicate id":   {"servers:\n  - id: a\n    install_path: a\n  - id: a\n    install_path: b\n", "more than once"},
		"same path":      {"servers:\n  - id: a\n    install_path: x\n  - id: b\n    install_path: ./x\n", "same install_path"},
		"no install dir": {"servers:\n  - id: a\n", "no install_path"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := readConfig(t, tc.yaml)

			if err == nil || !strings.Contains(err.Error(), tc.errContains) {
				t.Fatalf("expected error containing %q, got %v", tc.errContains, err)
			}
		})
	}
}
