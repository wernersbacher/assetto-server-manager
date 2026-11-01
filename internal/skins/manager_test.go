package skins

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/serverenv/envtest"
)

type testEnv struct {
	m        *Manager
	install  string
	settings smmodel.SkinSettings
	teams    map[uuid.UUID][]uuid.UUID
	free     uint64
	cleanup  func()
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	store, cleanupStore := envtest.TempJSONStore()

	install, err := ioutil.TempDir("", "asm-skins-test")

	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(install, "content", "cars", "ferrari", "skins"), 0755); err != nil {
		t.Fatal(err)
	}

	e := &testEnv{install: install, teams: map[uuid.UUID][]uuid.UUID{}, free: 100 << 30}

	e.m = NewManager(store, func() string { return install }, func() smmodel.SkinSettings { return e.settings }, func(a uuid.UUID) []uuid.UUID { return e.teams[a] })
	e.m.diskFree = func(string) (uint64, error) { return e.free, nil }
	e.cleanup = func() { cleanupStore(); _ = os.RemoveAll(install) }

	return e
}

func skinFiles(folder string, sizes ...int) []File {
	var files []File

	names := []string{"livery.dds", "preview.jpg", "ui_skin.json", "sponsors.png"}

	for i, size := range sizes {
		data := bytes.Repeat([]byte("x"), size)

		files = append(files, File{
			Path: folder + "/" + names[i%len(names)],
			Size: int64(size),
			Open: func() (io.ReadCloser, error) { return ioutil.NopCloser(bytes.NewReader(data)), nil },
		})
	}

	return files
}

func exists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func TestUpload_Permissions(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	user := Actor{ID: uuid.New()}
	admin := Actor{ID: uuid.New(), Writer: true}

	if _, err := e.m.Upload(Actor{}, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("blue", 10)); err != ErrNotAllowed {
		t.Errorf("anonymous: %v", err)
	}

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("blue", 10)); err != ErrUploadsDisabled {
		t.Errorf("uploads off: %v", err)
	}

	e.settings.UserUploads = true

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinGlobal, uuid.Nil, skinFiles("blue", 10)); err != ErrGlobalDisabled {
		t.Errorf("global off: %v", err)
	}

	team := uuid.New()

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinTeam, team, skinFiles("blue", 10)); err != ErrNotTeamMember {
		t.Errorf("not a member: %v", err)
	}

	e.teams[user.ID] = []uuid.UUID{team}

	skins, err := e.m.Upload(user, "ferrari", smmodel.SkinTeam, team, skinFiles("blue", 10))

	if err != nil || len(skins) != 1 {
		t.Fatalf("team upload: %v", err)
	}

	// private and team skins are not in the skins directory of the car
	if exists(filepath.Join(e.m.GlobalDir("ferrari"), skins[0].Folder)) || !exists(e.m.Dir(skins[0])) {
		t.Errorf("a team skin belongs into the user skins directory: %s", e.m.Dir(skins[0]))
	}

	if !strings.HasPrefix(skins[0].Folder, "u") || skins[0].Name != "blue" || skins[0].Visibility != smmodel.SkinTeam || skins[0].Owner != user.ID {
		t.Errorf("unexpected record: %+v", skins[0])
	}

	// admins upload global with their own name, even with uploads off for users
	e.settings = smmodel.SkinSettings{}

	skins, err = e.m.Upload(admin, "ferrari", smmodel.SkinGlobal, uuid.Nil, skinFiles("Official Red", 10))

	if err != nil || skins[0].Folder != "Official Red" || !exists(filepath.Join(e.m.GlobalDir("ferrari"), "Official Red", "livery.dds")) {
		t.Fatalf("admin global upload: %v %+v", err, skins)
	}

	if _, err := e.m.Upload(admin, "nocar", smmodel.SkinGlobal, uuid.Nil, skinFiles("x", 1)); !errors.Is(err, ErrInvalidSkin) {
		t.Errorf("unknown car: %v", err)
	}
}

func TestUpload_Validation(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	e.settings = smmodel.SkinSettings{UserUploads: true, MaxFiles: 3, MaxSkinMB: 1}
	user := Actor{ID: uuid.New()}

	for name, files := range map[string][]File{
		"no files":        nil,
		"no folder":       {{Path: "livery.dds", Size: 1}},
		"bad name":        skinFiles("bad:name", 1),
		"colon in file":   {{Path: "ok/a:b.dds", Size: 1}},
		"too many files":  skinFiles("many", 1, 1, 1, 1),
		"too large":       skinFiles("big", 2<<20),
		"bad extension":   {{Path: "ok/run.exe", Size: 1}},
		"empty skin name": {{Path: " /a.dds", Size: 1}},
	} {
		if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, files); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("big", 2<<20)); !errors.Is(err, ErrSkinTooLarge) {
		t.Errorf("expected ErrSkinTooLarge, got %v", err)
	}

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("many", 1, 1, 1, 1)); !errors.Is(err, ErrTooManyFiles) {
		t.Errorf("expected ErrTooManyFiles, got %v", err)
	}

	// nothing was left behind
	if entries, _ := ioutil.ReadDir(filepath.Join(e.install, "content", "user-skins", ".staging")); len(entries) != 0 {
		t.Errorf("staging not cleaned: %d entries", len(entries))
	}

	if skins, _ := e.m.store.ListUserSkins(); len(skins) != 0 {
		t.Errorf("failed uploads must not leave records: %d", len(skins))
	}

	// a lying size is caught while writing
	liar := File{Path: "liar/livery.dds", Size: 1, Open: func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 2<<20))), nil
	}}

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, []File{liar}); !errors.Is(err, ErrSkinTooLarge) {
		t.Errorf("a wrong declared size must be caught: %v", err)
	}

	if skins, _ := e.m.store.ListUserSkins(); len(skins) != 0 {
		t.Error("no record for a refused skin")
	}
}

func TestUpload_QuotaAndReplace(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	e.settings = smmodel.SkinSettings{UserUploads: true, UserQuotaMB: 1}
	user, other := Actor{ID: uuid.New()}, Actor{ID: uuid.New()}

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("a", 600<<10)); err != nil {
		t.Fatal(err)
	}

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("b", 600<<10)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected the quota to stop the second skin, got %v", err)
	}

	// the quota is per user
	if _, err := e.m.Upload(other, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("b", 600<<10)); err != nil {
		t.Errorf("another user has an own quota: %v", err)
	}

	// replacing a skin only counts the new size
	skins, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("a", 900<<10))

	if err != nil {
		t.Fatalf("replace within quota: %v", err)
	}

	if used, _ := e.m.Usage(user.ID); used != 900<<10 {
		t.Errorf("usage after replacing: %d", used)
	}

	if list, _ := e.m.store.ListUserSkins(); len(list) != 2 {
		t.Errorf("replacing must not add a record, have %d", len(list))
	}

	// a writer is not held to the quota
	admin := Actor{ID: uuid.New(), Writer: true}

	if _, err := e.m.Upload(admin, "ferrari", smmodel.SkinGlobal, uuid.Nil, skinFiles("huge", 2<<20)); err != nil {
		t.Errorf("writers have no quota: %v", err)
	}

	// the folder name hides the owner: same name, different folder
	otherSkins, _ := e.m.store.ListUserSkins()

	folders := map[string]bool{}

	for _, s := range otherSkins {
		folders[s.Folder] = true
	}

	if len(folders) != 3 || skins[0].Folder == smmodel.UserSkinFolder(other.ID, "a") {
		t.Errorf("folders must be unique per owner: %v", folders)
	}
}

func TestUpload_FreeDiskReserve(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	e.settings = smmodel.SkinSettings{UserUploads: true, MinFreeMB: 100}
	user := Actor{ID: uuid.New()}

	e.free = 100<<20 + 1000

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("a", 2000)); err != ErrDiskFull {
		t.Errorf("an upload that eats into the reserve must fail, got %v", err)
	}

	if err := e.m.CheckSpace(500); err != nil {
		t.Errorf("a small request fits: %v", err)
	}

	if err := e.m.CheckSpace(5000); err != ErrDiskFull {
		t.Errorf("a big request must not: %v", err)
	}

	e.free = 100<<20 + 3000

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("a", 2000)); err != nil {
		t.Errorf("enough space: %v", err)
	}

	// admins are limited by the disk too
	e.free = 1 << 20

	if _, err := e.m.Upload(Actor{ID: uuid.New(), Writer: true}, "ferrari", smmodel.SkinGlobal, uuid.Nil, skinFiles("a", 10)); err != ErrDiskFull {
		t.Errorf("admins must not fill the disk either: %v", err)
	}

	// no way to read the free space: refuse
	e.m.diskFree = func(string) (uint64, error) { return 0, errors.New("boom") }

	if _, err := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("c", 10)); err == nil {
		t.Error("unknown free space must refuse the upload")
	}
}

func TestSetVisibilityMovesFiles(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	e.settings = smmodel.SkinSettings{UserUploads: true}
	owner, stranger := Actor{ID: uuid.New()}, Actor{ID: uuid.New()}
	admin := Actor{ID: uuid.New(), Writer: true}
	team := uuid.New()

	skins, err := e.m.Upload(owner, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("mine", 10, 10))

	if err != nil {
		t.Fatal(err)
	}

	id := skins[0].ID.String()
	privateDir := e.m.Dir(skins[0])

	if err := e.m.SetVisibility(stranger, id, smmodel.SkinPrivate, uuid.Nil); err != ErrNotAllowed {
		t.Errorf("stranger: %v", err)
	}

	if err := e.m.SetVisibility(owner, id, smmodel.SkinGlobal, uuid.Nil); err != ErrGlobalDisabled {
		t.Errorf("global off: %v", err)
	}

	if err := e.m.SetVisibility(owner, id, smmodel.SkinTeam, team); err != ErrNotTeamMember {
		t.Errorf("not a member: %v", err)
	}

	e.teams[owner.ID] = []uuid.UUID{team}

	if err := e.m.SetVisibility(owner, id, smmodel.SkinTeam, team); err != nil {
		t.Fatal(err)
	}

	if got, _ := e.m.Find(id); got.Visibility != smmodel.SkinTeam || got.TeamID != team || !exists(privateDir) {
		t.Errorf("team: %+v", got)
	}

	e.settings.GlobalShare = true

	if err := e.m.SetVisibility(owner, id, smmodel.SkinGlobal, uuid.Nil); err != nil {
		t.Fatal(err)
	}

	got, _ := e.m.Find(id)
	globalDir := filepath.Join(e.m.GlobalDir("ferrari"), got.Folder)

	if got.Visibility != smmodel.SkinGlobal || got.TeamID != uuid.Nil || exists(privateDir) || !exists(filepath.Join(globalDir, "livery.dds")) {
		t.Errorf("global: files not moved: %+v", got)
	}

	// taking it back is always possible, even with sharing turned off
	e.settings = smmodel.SkinSettings{}

	if err := e.m.SetVisibility(owner, id, smmodel.SkinPrivate, uuid.Nil); err != nil {
		t.Fatal(err)
	}

	if exists(globalDir) || !exists(privateDir) {
		t.Error("files must move back")
	}

	// a collision at the target blocks the move and keeps everything
	e.settings = smmodel.SkinSettings{UserUploads: true, GlobalShare: true}

	if err := os.MkdirAll(globalDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := e.m.SetVisibility(owner, id, smmodel.SkinGlobal, uuid.Nil); err != ErrNameTaken {
		t.Errorf("collision: %v", err)
	}

	if got, _ := e.m.Find(id); got.Visibility != smmodel.SkinPrivate || !exists(privateDir) {
		t.Error("a failed move must change nothing")
	}

	// admins can change every skin
	_ = os.RemoveAll(globalDir)

	if err := e.m.SetVisibility(admin, id, smmodel.SkinGlobal, uuid.Nil); err != nil {
		t.Errorf("admin: %v", err)
	}

	if err := e.m.SetVisibility(admin, uuid.New().String(), smmodel.SkinGlobal, uuid.Nil); err != smmodel.ErrUserSkinNotFound {
		t.Errorf("unknown skin: %v", err)
	}

	if err := e.m.SetVisibility(owner, id, "bogus", uuid.Nil); err == nil {
		t.Error("an unknown visibility must fail")
	}
}

func TestDelete(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	e.settings = smmodel.SkinSettings{UserUploads: true}
	owner, stranger := Actor{ID: uuid.New()}, Actor{ID: uuid.New()}

	skins, err := e.m.Upload(owner, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("mine", 10))

	if err != nil {
		t.Fatal(err)
	}

	id, dir := skins[0].ID.String(), e.m.Dir(skins[0])

	if err := e.m.Delete(stranger, id); err != ErrNotAllowed || !exists(dir) {
		t.Errorf("stranger: %v", err)
	}

	if err := e.m.Delete(Actor{ID: stranger.ID, Writer: true}, id); err != nil || exists(dir) {
		t.Fatalf("admin delete: %v", err)
	}

	if _, err := e.m.Find(id); err != smmodel.ErrUserSkinNotFound {
		t.Errorf("record must be gone: %v", err)
	}

	// the folder of a global skin deleted by name drops its record
	admin := Actor{ID: uuid.New(), Writer: true}

	if _, err := e.m.Upload(admin, "ferrari", smmodel.SkinGlobal, uuid.Nil, skinFiles("pub", 10)); err != nil {
		t.Fatal(err)
	}

	if err := e.m.ForgetFolder("ferrari", "pub"); err != nil {
		t.Fatal(err)
	}

	if rec, _ := e.m.ByFolder("ferrari", "pub"); rec != nil {
		t.Error("ForgetFolder must drop the record")
	}

}

func TestUpload_PathTraversalStaysInside(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	e.settings = smmodel.SkinSettings{UserUploads: true}

	files := []File{{Path: "../../../evil/livery.dds", Size: 1, Open: func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewReader([]byte("x"))), nil
	}}}

	skins, err := e.m.Upload(Actor{ID: uuid.New()}, "ferrari", smmodel.SkinPrivate, uuid.Nil, files)

	if err != nil || len(skins) != 1 || skins[0].Name != "evil" {
		t.Fatalf("a traversing path is cleaned to a skin named evil: %v %+v", err, skins)
	}

	if !exists(filepath.Join(e.m.UserDir("ferrari"), skins[0].Folder, "livery.dds")) || exists(filepath.Join(e.install, "evil")) {
		t.Error("the file must stay inside the user skins directory")
	}
}

func TestUsableSkinsAndChecks(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	e.settings = smmodel.SkinSettings{UserUploads: true, GlobalShare: true}
	owner, mate, stranger := Actor{ID: uuid.New()}, uuid.New(), uuid.New()
	team := uuid.New()
	e.teams[owner.ID] = []uuid.UUID{team}
	e.teams[mate] = []uuid.UUID{team}

	mine, _ := e.m.Upload(owner, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("mine", 10))
	shared, _ := e.m.Upload(owner, "ferrari", smmodel.SkinTeam, team, skinFiles("shared", 10))
	public, _ := e.m.Upload(owner, "ferrari", smmodel.SkinGlobal, uuid.Nil, skinFiles("public", 10))

	for who, want := range map[uuid.UUID][]string{
		owner.ID: {mine[0].Folder, shared[0].Folder},
		mate:     {shared[0].Folder},
		stranger: nil,
		uuid.Nil: nil,
	} {
		usable, err := e.m.UsableSkins(who)

		if err != nil {
			t.Fatal(err)
		}

		got := usable["ferrari"]

		if len(got) != len(want) {
			t.Errorf("%v: usable %v, want %v", who, got, want)
		}

		for _, folder := range want {
			found := false

			for _, g := range got {
				found = found || g == folder
			}

			if !found {
				t.Errorf("%v: %s missing from %v", who, folder, got)
			}
		}
	}

	// global skins live in the skins directory and are not listed as extra skins
	if usable, _ := e.m.UsableSkins(owner.ID); len(usable["ferrari"]) != 2 {
		t.Errorf("global skins must not be listed: %v", usable)
	}

	for _, c := range []struct {
		skin    string
		account uuid.UUID
		ok      bool
	}{
		{mine[0].Folder, owner.ID, true},
		{mine[0].Folder, mate, false},
		{mine[0].Folder, uuid.Nil, false},
		{shared[0].Folder, mate, true},
		{shared[0].Folder, stranger, false},
		{public[0].Folder, stranger, true},
		{"unknown_skin_from_disk", stranger, true},
		{"", stranger, true},
		{"../x", stranger, true},
	} {
		if err := e.m.CheckUsable("ferrari", c.skin, c.account); (err == nil) != c.ok {
			t.Errorf("CheckUsable(%q, %v) = %v, want ok=%v", c.skin, c.account, err, c.ok)
		}
	}

	// Resolve only finds private and team skins with files
	if rec, dir, _ := e.m.Resolve("ferrari", mine[0].Folder); rec == nil || !exists(dir) {
		t.Error("Resolve must find a private skin")
	}

	if rec, _, _ := e.m.Resolve("ferrari", public[0].Folder); rec != nil {
		t.Error("Resolve must not return global skins")
	}

	if rec, _, _ := e.m.Resolve("ferrari", "../mine"); rec != nil {
		t.Error("Resolve must refuse bad names")
	}

	// the list for the car page
	list, _ := e.m.ListForCar("ferrari", Actor{ID: stranger})

	if len(list) != 1 || list[0].Folder != public[0].Folder {
		t.Errorf("a stranger sees the global skins only: %v", list)
	}

	if list, _ := e.m.ListForCar("ferrari", Actor{ID: uuid.New(), Writer: true}); len(list) != 3 {
		t.Errorf("a writer sees all skins: %d", len(list))
	}
}

func TestForgetCar(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	if err := os.MkdirAll(filepath.Join(e.install, "content", "cars", "bmw"), 0755); err != nil {
		t.Fatal(err)
	}

	e.settings = smmodel.SkinSettings{UserUploads: true}
	user := Actor{ID: uuid.New()}

	ferrari, _ := e.m.Upload(user, "ferrari", smmodel.SkinPrivate, uuid.Nil, skinFiles("a", 10))
	bmw, _ := e.m.Upload(user, "bmw", smmodel.SkinPrivate, uuid.Nil, skinFiles("b", 10))

	if err := e.m.ForgetCar("ferrari"); err != nil {
		t.Fatal(err)
	}

	if _, err := e.m.Find(ferrari[0].ID.String()); err != smmodel.ErrUserSkinNotFound || exists(e.m.UserDir("ferrari")) {
		t.Error("the skins of the deleted car must be gone")
	}

	if _, err := e.m.Find(bmw[0].ID.String()); err != nil || !exists(e.m.Dir(bmw[0])) {
		t.Error("skins of other cars must stay")
	}

	if err := e.m.ForgetCar("../bmw"); err != nil || !exists(e.m.Dir(bmw[0])) {
		t.Error("a bad car name must do nothing")
	}
}

func TestSkinIDsFromOutsideNeverReachTheFileSystem(t *testing.T) {
	e := newTestEnv(t)
	defer e.cleanup()

	admin := Actor{ID: uuid.New(), Writer: true}

	for _, id := range []string{"../../x", `..\x`, "", "not-a-uuid", "00000000-0000-0000-0000-000000000000/../x"} {
		if _, err := e.m.Find(id); err != smmodel.ErrUserSkinNotFound {
			t.Errorf("Find(%q): %v", id, err)
		}

		if err := e.m.Delete(admin, id); err != smmodel.ErrUserSkinNotFound {
			t.Errorf("Delete(%q): %v", id, err)
		}

		if err := e.m.SetVisibility(admin, id, smmodel.SkinGlobal, uuid.Nil); err != smmodel.ErrUserSkinNotFound {
			t.Errorf("SetVisibility(%q): %v", id, err)
		}
	}
}

func TestMessage(t *testing.T) {
	err := refuse(ErrTooManyFiles, i18n.Msg("text.skins.too_many_files_limit", "the skin has too many files ({files} files, at most {max})", "files", 3, "max", 2))

	if !errors.Is(err, ErrTooManyFiles) || !IsUserError(err) {
		t.Fatal("a refusal must be its sentinel error")
	}

	if got := Message(err).String(); got != "the skin has too many files (3 files, at most 2)" {
		t.Errorf("unexpected message %q", got)
	}

	if got := Message(ErrDiskFull); got.Key != "text.skins.disk_full" {
		t.Errorf("unexpected message %+v", got)
	}

	if got := Message(errors.New("skins: other")).String(); got != "other" {
		t.Errorf("unexpected message %q", got)
	}
}
