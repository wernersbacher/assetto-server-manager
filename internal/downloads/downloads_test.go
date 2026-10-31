package downloads

import (
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

func TestSkinSignature(t *testing.T) {
	s := NewSignerWithSecret([]byte("0123456789abcdef0123456789abcdef"))
	now := time.Unix(1000000, 0)
	expires := now.Add(time.Hour)

	q, err := s.SkinQuery("car", "skin", expires)

	if err != nil {
		t.Fatal(err)
	}

	if !s.VerifySkin(q, "car", "skin", now) {
		t.Error("valid signature rejected")
	}

	if s.VerifySkin(q, "car", "skin", expires.Add(time.Second)) {
		t.Error("expired signature accepted")
	}

	if !s.VerifySkin(q, "car", "skin", expires) {
		t.Error("signature must be valid until the expiry second")
	}

	if s.VerifySkin(q, "car", "other", now) || s.VerifySkin(q, "other", "skin", now) {
		t.Error("signature accepted for another skin or car")
	}

	tampered := url.Values{"expires": {"9999999999"}, "sig": {q.Get("sig")}}

	if s.VerifySkin(tampered, "car", "skin", now) {
		t.Error("signature accepted with a changed expiry")
	}

	flipped := url.Values{"expires": q["expires"], "sig": {"00" + q.Get("sig")[2:]}}

	if s.VerifySkin(flipped, "car", "skin", now) {
		t.Error("manipulated signature accepted")
	}

	for _, bad := range []url.Values{{}, {"expires": {"x"}, "sig": {"zz"}}, {"expires": q["expires"]}, {"expires": q["expires"], "sig": {"nothex"}}} {
		if s.VerifySkin(bad, "car", "skin", now) {
			t.Errorf("%v accepted", bad)
		}
	}

	other := NewSignerWithSecret([]byte("another secret another secret!!!"))

	if other.VerifySkin(q, "car", "skin", now) {
		t.Error("signature accepted with another secret")
	}
}

func TestSignerSecretPersists(t *testing.T) {
	dir, err := ioutil.TempDir("", "downloads-secret")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	store := smstore.NewJSONStore(filepath.Join(dir, "store"), filepath.Join(dir, "shared"))
	expires := time.Now().Add(time.Hour)

	q, err := NewSigner(store).SkinQuery("car", "skin", expires)

	if err != nil {
		t.Fatal(err)
	}

	// a new signer (restart) reads the same secret
	if !NewSigner(store).VerifySkin(q, "car", "skin", time.Now()) {
		t.Error("signature does not survive a new signer on the same store")
	}
}

func TestSkinURL(t *testing.T) {
	s := NewSignerWithSecret([]byte("0123456789abcdef0123456789abcdef"))

	u, err := s.SkinURL("https://example.org/", "car_1", "skin one", time.Unix(2000000000, 0))

	if err != nil {
		t.Fatal(err)
	}

	parsed, err := url.Parse(u)

	if err != nil {
		t.Fatal(err)
	}

	if parsed.Path != "/car/car_1/skin/skin one/download" || parsed.Host != "example.org" {
		t.Errorf("unexpected URL %s", u)
	}

	if !s.VerifySkin(parsed.Query(), "car_1", "skin one", time.Unix(1999999999, 0)) {
		t.Error("URL signature does not verify")
	}
}

func TestIsUIAsset(t *testing.T) {
	for p, want := range map[string]bool{
		"/content/cars/x/ui/badge.png":          true,
		"/content/cars/x/skins/y/preview.JPG":   true,
		"/content/cars/x/ui/ui_car.json":        true,
		"/content/tracks/t/ui/ui_track.json":    true,
		"/content/server-manager/anything.bin":  true,
		"/content/cars/x/x.kn5":                 false,
		"/content/cars/x/data.acd":              false,
		"/content/cars/x/skins/y/body.dds":      false,
		"/content/cars/x/extension/config.json": false,
		"/content/tracks/t/t.kn5":               false,
	} {
		if got := IsUIAsset(p); got != want {
			t.Errorf("IsUIAsset(%s) = %t, want %t", p, got, want)
		}
	}
}

func TestSkinVersion(t *testing.T) {
	dir, err := ioutil.TempDir("", "skin-version")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(dir)

	write := func(name, content string) {
		if err := ioutil.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("a.dds", "1")

	v1, err := SkinVersion(dir)

	if err != nil {
		t.Fatal(err)
	}

	if v2, _ := SkinVersion(dir); v1 != v2 || len(v1) != 16 {
		t.Errorf("version not stable or wrong length: %q %q", v1, v2)
	}

	write("b.dds", "22")

	if v3, _ := SkinVersion(dir); v3 == v1 {
		t.Error("version did not change after adding a file")
	}
}
