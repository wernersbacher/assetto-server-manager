package server

import (
	"encoding/json"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	"github.com/wernersbacher/ACPitwall/internal/downloads"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
)

func TestCMContentJSONShape(t *testing.T) {
	content := CMContent{
		Cars: map[string]ContentURL{
			"car_a": {URL: "https://dl/car_a.zip", Skins: map[string]SkinEntry{"red": {URL: "https://x/red", Version: "abc"}}},
			"car_b": {URL: ""},
		},
		Track: ContentURL{URL: "https://dl/track.zip"},
	}

	b, err := json.Marshal(content)

	if err != nil {
		t.Fatal(err)
	}

	expected := `{"cars":{"car_a":{"url":"https://dl/car_a.zip","skins":{"red":{"url":"https://x/red","version":"abc"}}},"car_b":{"url":""}},"track":{"url":"https://dl/track.zip"},"password":false}`

	if string(b) != expected {
		t.Errorf("content.json shape changed:\n got %s\nwant %s", b, expected)
	}
}

func TestCMContentSkins(t *testing.T) {
	dir, err := ioutil.TempDir("", "cmw-skins")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	for _, skin := range []string{"red", "blue"} {
		p := filepath.Join(dir, "content", "cars", "car_a", "skins", skin)

		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}

		if err := ioutil.WriteFile(filepath.Join(p, "ui_skin.json"), []byte(skin), 0644); err != nil {
			t.Fatal(err)
		}
	}

	config := &smconfig.Configuration{}
	config.HTTP.BaseURL = "https://pit.example"

	cmw := NewContentManagerWrapper(config, nil, nil, nil, &serverenv.Env{
		InstallPath: func() string { return dir },
		ServerID:    func() smmodel.ServerID { return "srv" },
	})

	access := downloads.Public
	signer := downloads.NewSignerWithSecret([]byte("0123456789abcdef0123456789abcdef"))
	cmw.SetSkinDownloads(signer, func() downloads.Access { return access })

	entry := func(model, skin string) *serverconfig.Entrant {
		e := serverconfig.NewEntrant()
		e.Model, e.Skin = model, skin

		return e
	}

	cmw.entryList = serverconfig.EntryList{
		"CAR_0": entry("car_a", "red"),
		"CAR_1": entry("car_a", "red"),
		"CAR_2": entry("car_a", "missing"),
		"CAR_3": entry("car_a", "../red"),
		"CAR_4": entry("car_a", ""),
		"CAR_5": entry("car_b", "red"),
	}

	now := time.Now()
	skins := cmw.skinEntries(now)

	if len(skins) != 1 || len(skins["car_a"]) != 1 {
		t.Fatalf("expected only car_a/red, got %+v", skins)
	}

	red := skins["car_a"]["red"]

	if red.Version == "" {
		t.Error("missing skin version")
	}

	u, err := url.Parse(red.URL)

	if err != nil || u.Host != "pit.example" || u.Path != "/car/car_a/skin/red/download" {
		t.Fatalf("unexpected skin url %q (%v)", red.URL, err)
	}

	if !signer.VerifySkin(u.Query(), "car_a", "red", now) {
		t.Error("skin url signature invalid")
	}

	if signer.VerifySkin(u.Query(), "car_a", "red", now.Add(downloads.DefaultTTL+time.Minute)) {
		t.Error("skin url must expire")
	}

	// downloads off: no skin urls
	access = downloads.Off

	if got := cmw.skinEntries(now); len(got) != 0 {
		t.Errorf("expected no skins with downloads off, got %+v", got)
	}

	// without a base URL no absolute url can be built
	access = downloads.Public
	config.HTTP.BaseURL = ""

	if got := cmw.skinEntries(now); len(got) != 0 {
		t.Errorf("expected no skins without base URL, got %+v", got)
	}
}
