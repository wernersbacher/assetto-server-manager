package serverconfig

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAndReadString(t *testing.T) {
	dir, err := ioutil.TempDir("", "serverconfig")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sc := ConfigIniDefault()

	if err := sc.Write(dir); err != nil {
		t.Fatal(err)
	}

	content, err := sc.ReadString(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "[SERVER]") || !strings.Contains(content, "WELCOME_MESSAGE="+MOTDFilename) {
		t.Errorf("unexpected server_cfg.ini content: %s", content)
	}

	entrant := NewEntrant()
	entrant.Name = "Driver"
	entrant.Model = "ks_mazda_miata"

	entryList := EntryList{}
	entryList.AddToBackOfGrid(entrant)

	if err := entryList.Write(filepath.Join(dir, "setups"), dir); err != nil {
		t.Fatal(err)
	}

	content, err = entryList.ReadString(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(content, "[CAR_0]") || !strings.Contains(content, "DRIVERNAME=Driver") {
		t.Errorf("unexpected entry_list.ini content: %s", content)
	}
}
