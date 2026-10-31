// Package downloads holds the access rules for content downloads (content directory, setups, skin zips) and the
// signed skin download URLs that Content Manager (which sends no cookies) can use.
package downloads

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

// Access says who may download content, setups and skins. The zero value is Public, which is the behaviour from
// before the option existed, so stored options need no migration.
type Access int

const (
	// Public: everybody who can read the pages (on an open server also anonymous visitors).
	Public Access = iota
	// LoggedIn: only logged in accounts, even on an open server.
	LoggedIn
	// Off: only admins; also no signed skin URLs are handed out.
	Off
)

func (a Access) String() string {
	switch a {
	case LoggedIn:
		return "login"
	case Off:
		return "off"
	default:
		return "public"
	}
}

// ParseAccess is the inverse of Access.String.
func ParseAccess(s string) (Access, bool) {
	switch s {
	case "public":
		return Public, true
	case "login":
		return LoggedIn, true
	case "off":
		return Off, true
	}

	return Public, false
}

// IsUIAsset reports whether a path below /content is something the pages themselves show (images, ui_*.json) rather
// than a content download (kn5, data.acd, textures, ...). UI assets stay readable for everyone with read access, so
// the pages of an open server keep their pictures when downloads are restricted.
func IsUIAsset(p string) bool {
	p = strings.ToLower(p)

	if strings.HasPrefix(p, "/content/server-manager/") {
		return true
	}

	switch filepath.Ext(p) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico":
		return true
	case ".json":
		base := p[strings.LastIndex(p, "/")+1:]
		return strings.HasPrefix(base, "ui_")
	}

	return false
}

const (
	secretMetaKey = "download-signing-secret"

	// DefaultTTL is how long a signed skin URL is valid.
	DefaultTTL = 6 * time.Hour
)

// Signer creates and verifies signed skin download URLs. The secret is generated once and persisted in the store.
type Signer struct {
	store smstore.Store

	mutex  sync.Mutex
	secret []byte
}

func NewSigner(store smstore.Store) *Signer {
	return &Signer{store: store}
}

// NewSignerWithSecret is for tests.
func NewSignerWithSecret(secret []byte) *Signer {
	return &Signer{secret: secret}
}

func (s *Signer) loadSecret() ([]byte, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if len(s.secret) > 0 {
		return s.secret, nil
	}

	var stored string

	err := s.store.GetMeta(secretMetaKey, &stored)

	if err == nil {
		if secret, err := hex.DecodeString(stored); err == nil && len(secret) >= 16 {
			s.secret = secret
			return secret, nil
		}
	} else if err != smstore.ErrValueNotSet {
		return nil, err
	}

	secret := make([]byte, 32)

	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}

	if err := s.store.SetMeta(secretMetaKey, hex.EncodeToString(secret)); err != nil {
		return nil, err
	}

	s.secret = secret

	return secret, nil
}

func signature(secret []byte, car, skin string, expires int64) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "skin\x00%s\x00%s\x00%d", car, skin, expires)

	return hex.EncodeToString(mac.Sum(nil))
}

// SkinQuery returns the query string (expires, sig) that authorizes the download of one skin until expires.
func (s *Signer) SkinQuery(car, skin string, expires time.Time) (url.Values, error) {
	secret, err := s.loadSecret()

	if err != nil {
		return nil, err
	}

	v := url.Values{}
	v.Set("expires", strconv.FormatInt(expires.Unix(), 10))
	v.Set("sig", signature(secret, car, skin, expires.Unix()))

	return v, nil
}

// SkinURL builds the absolute signed download URL of a skin.
func (s *Signer) SkinURL(baseURL, car, skin string, expires time.Time) (string, error) {
	q, err := s.SkinQuery(car, skin, expires)

	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s/car/%s/skin/%s/download?%s", strings.TrimRight(baseURL, "/"), url.PathEscape(car), url.PathEscape(skin), q.Encode()), nil
}

// VerifySkin checks the expires and sig query values of a request for the given skin. It is false for missing,
// manipulated (other car or skin, changed expiry or signature) and expired signatures.
func (s *Signer) VerifySkin(q url.Values, car, skin string, now time.Time) bool {
	expires, err := strconv.ParseInt(q.Get("expires"), 10, 64)

	if err != nil || now.Unix() > expires {
		return false
	}

	sig, err := hex.DecodeString(q.Get("sig"))

	if err != nil {
		return false
	}

	secret, err := s.loadSecret()

	if err != nil {
		return false
	}

	expected, _ := hex.DecodeString(signature(secret, car, skin, expires))

	return hmac.Equal(sig, expected)
}

// SkinStats counts the regular files of a skin directory and their total size. Symbolic links are skipped.
func SkinStats(skinDir string) (files int, size int64, err error) {
	err = filepath.Walk(skinDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			files++
			size += info.Size()
		}

		return nil
	})

	return files, size, err
}

// SkinVersion is the version Content Manager compares to decide whether it has to download a skin again: a hash of
// the relative names, sizes and modification times of the skin files (not of their contents, so it is cheap enough to
// compute on every details request).
func SkinVersion(skinDir string) (string, error) {
	type entry struct {
		rel  string
		size int64
		mod  int64
	}

	var entries []entry

	err := filepath.Walk(skinDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		rel, err := filepath.Rel(skinDir, p)

		if err != nil {
			return err
		}

		entries = append(entries, entry{filepath.ToSlash(rel), info.Size(), info.ModTime().UnixNano()})

		return nil
	})

	if err != nil {
		return "", err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	h := sha1.New()

	for _, e := range entries {
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d\n", e.rel, e.size, e.mod)
	}

	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
