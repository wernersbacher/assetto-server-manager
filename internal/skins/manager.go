// Package skins manages the car skins that accounts upload. A skin is private (its owner), shared with a team or
// global. Global skins live in the skins directory of the car like the skins an admin always uploaded; private and team
// skins live in content/user-skins/<car>/<folder>, so they do not show up for anybody else. Metadata (owner,
// visibility, size) is stored in the Store (model.UserSkin).
package skins

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/plainname"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
)

var (
	ErrUploadsDisabled = errors.New("skins: skin uploads by users are disabled")
	ErrGlobalDisabled  = errors.New("skins: sharing skins globally is disabled")
	ErrNotAllowed      = errors.New("skins: you are not allowed to change this skin")
	ErrNotTeamMember   = errors.New("skins: you are not a member of this team")
	ErrInvalidSkin     = errors.New("skins: invalid skin upload")
	ErrTooManyFiles    = errors.New("skins: the skin has too many files")
	ErrSkinTooLarge    = errors.New("skins: the skin is too large")
	ErrQuotaExceeded   = errors.New("skins: your skin storage is full")
	ErrDiskFull        = errors.New("skins: not enough free disk space on the server")
	ErrNameTaken       = errors.New("skins: a skin with this name exists already")
)

// maxSkinsPerUpload limits how many skin folders one request may contain.
const maxSkinsPerUpload = 20

// userSkinNamePattern is the name a user can give a skin (the name of the uploaded folder).
var userSkinNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.\-]{0,63}$`)

// userFileExtensions are the file types accounts without write access may upload in a skin.
var userFileExtensions = map[string]bool{
	".dds": true, ".png": true, ".jpg": true, ".jpeg": true, ".json": true, ".ini": true, ".txt": true, ".bmp": true, ".tga": true,
}

// Actor is who uploads or changes a skin. A zero ID is an anonymous visitor, who can do nothing.
type Actor struct {
	ID uuid.UUID
	// Writer is true for accounts with write access (admins): they upload global skins always, are not held to the
	// user quota and may change every skin.
	Writer bool
}

// File is one file of an upload. Path is relative to the car: "<skin folder>/<file>".
type File struct {
	Path string
	Size int64
	Open func() (io.ReadCloser, error)
}

// FilesFromMultipart converts the files of a multipart form. Browsers send the folder as part of the file name, but
// since Go 1.17 multipart.FileHeader.Filename only has the base name, so the full name comes from the
// Content-Disposition header.
func FilesFromMultipart(form map[string][]*multipart.FileHeader) []File {
	var files []File

	for _, headers := range form {
		for _, header := range headers {
			header := header

			filename := header.Filename

			if _, params, err := mime.ParseMediaType(header.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
				filename = params["filename"]
			}

			files = append(files, File{
				Path: filename,
				Size: header.Size,
				Open: func() (io.ReadCloser, error) { return header.Open() },
			})
		}
	}

	return files
}

// Manager stores and changes uploaded skins.
type Manager struct {
	store       smstore.Store
	installPath func() string
	settings    func() smmodel.SkinSettings
	teamsOf     func(account uuid.UUID) []uuid.UUID

	now      func() time.Time
	diskFree func(path string) (uint64, error)
	mutex    sync.Mutex
}

// NewManager creates a manager. teamsOf returns the ids of the teams an account is a member of.
func NewManager(store smstore.Store, installPath func() string, settings func() smmodel.SkinSettings, teamsOf func(uuid.UUID) []uuid.UUID) *Manager {
	return &Manager{
		store:       store,
		installPath: installPath,
		settings:    settings,
		teamsOf:     teamsOf,
		now:         time.Now,
		diskFree:    diskFree,
	}
}

// UserDir is the directory of the private and team skins of a car.
func (m *Manager) UserDir(car string) string {
	return filepath.Join(m.installPath(), "content", "user-skins", car)
}

// GlobalDir is the skins directory of a car.
func (m *Manager) GlobalDir(car string) string {
	return filepath.Join(m.installPath(), "content", "cars", car, "skins")
}

// Dir is where the files of a skin are, according to its visibility.
func (m *Manager) Dir(skin *smmodel.UserSkin) string {
	if skin.Normalized() == smmodel.SkinGlobal {
		return filepath.Join(m.GlobalDir(skin.Car), skin.Folder)
	}

	return filepath.Join(m.UserDir(skin.Car), skin.Folder)
}

// Find returns a skin by its id.
func (m *Manager) Find(id string) (*smmodel.UserSkin, error) {
	id, err := canonicalID(id)

	if err != nil {
		return nil, err
	}

	return m.store.FindUserSkinByID(id)
}

// ByFolder returns the record of the skin with this folder name of a car, or nil if there is none (the skin was
// placed there by hand, or does not exist).
func (m *Manager) ByFolder(car, folder string) (*smmodel.UserSkin, error) {
	skins, err := m.store.ListUserSkins()

	if err != nil {
		return nil, err
	}

	for _, skin := range skins {
		if skin.Car == car && skin.Folder == folder {
			return skin, nil
		}
	}

	return nil, nil
}

// Usage returns how many bytes the skins of an owner take.
func (m *Manager) Usage(owner uuid.UUID) (int64, error) {
	skins, err := m.store.ListUserSkins()

	if err != nil {
		return 0, err
	}

	return usage(skins, owner, uuid.Nil), nil
}

// usage sums the skin sizes of an owner, leaving out the skin with the id skip.
func usage(skins []*smmodel.UserSkin, owner uuid.UUID, skip uuid.UUID) int64 {
	var total int64

	for _, skin := range skins {
		if skin.Owner == owner && skin.ID != skip {
			total += skin.Size
		}
	}

	return total
}

// authorize checks whether the actor may make a skin with this visibility.
func (m *Manager) authorize(actor Actor, vis smmodel.SkinVisibility, team uuid.UUID) error {
	if actor.ID == uuid.Nil {
		return ErrNotAllowed
	}

	settings := m.settings()

	if !actor.Writer && !settings.UserUploads {
		return ErrUploadsDisabled
	}

	switch vis {
	case smmodel.SkinPrivate:
	case smmodel.SkinGlobal:
		if !actor.Writer && !settings.GlobalShare {
			return ErrGlobalDisabled
		}
	case smmodel.SkinTeam:
		if team == uuid.Nil || !actor.Writer && !m.isMember(actor.ID, team) {
			return ErrNotTeamMember
		}
	default:
		return smmodel.ErrSkinVisibility
	}

	return nil
}

func (m *Manager) isMember(account, team uuid.UUID) bool {
	if m.teamsOf == nil {
		return false
	}

	for _, id := range m.teamsOf(account) {
		if id == team {
			return true
		}
	}

	return false
}

// CheckSpace refuses an upload of about size bytes up front, before the request body is read, if it would leave less
// than the configured free disk space.
func (m *Manager) CheckSpace(size int64) error {
	dir := filepath.Join(m.installPath(), "content", "user-skins")

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	free, err := m.diskFree(dir)

	if err != nil {
		return fmt.Errorf("could not determine the free disk space: %w", err)
	}

	if size < 0 {
		size = 0
	}

	need := uint64(size) + uint64(m.settings().Limits().MinFreeBytes)

	if free < need {
		return ErrDiskFull
	}

	return nil
}

type uploadGroup struct {
	name  string
	files []File
	size  int64
}

// groupFiles splits the files by their top folder: every folder is one skin.
func groupFiles(files []File) ([]*uploadGroup, error) {
	var groups []*uploadGroup

	byName := make(map[string]*uploadGroup)

	for _, file := range files {
		// cleaning the path as an absolute path removes any leading "../"
		cleaned := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(file.Path, "\\", "/")), "/")
		parts := strings.SplitN(cleaned, "/", 2)

		if len(parts) != 2 || parts[1] == "" || strings.ContainsAny(cleaned, ":\x00") {
			return nil, refuse(ErrInvalidSkin, i18n.Msg("text.skins.not_in_folder", "invalid skin upload: the files must be in a skin folder"))
		}

		group := byName[parts[0]]

		if group == nil {
			group = &uploadGroup{name: parts[0]}
			byName[parts[0]] = group
			groups = append(groups, group)
		}

		file.Path = parts[1]
		group.files = append(group.files, file)
		group.size += file.Size
	}

	if len(groups) == 0 {
		return nil, refuse(ErrInvalidSkin, i18n.Msg("text.skins.no_files", "invalid skin upload: no files"))
	}

	if len(groups) > maxSkinsPerUpload {
		return nil, refuse(ErrInvalidSkin, i18n.Msg("text.skins.too_many_skins", "invalid skin upload: at most {max} skins per upload", "max", maxSkinsPerUpload))
	}

	return groups, nil
}

// Upload stores the skins in files (one per top folder) for a car with the given visibility. Uploading an own skin
// again (same name) replaces it. All limits are checked before anything is written, and again while writing.
func (m *Manager) Upload(actor Actor, car string, vis smmodel.SkinVisibility, team uuid.UUID, files []File) ([]*smmodel.UserSkin, error) {
	if !plainname.Valid(car) {
		return nil, refuse(ErrInvalidSkin, i18n.Msg("text.skins.unknown_car", "invalid skin upload: unknown car"))
	}

	if info, err := os.Stat(filepath.Join(m.installPath(), "content", "cars", car)); err != nil || !info.IsDir() {
		return nil, refuse(ErrInvalidSkin, i18n.Msg("text.skins.unknown_car", "invalid skin upload: unknown car"))
	}

	if err := m.authorize(actor, vis, team); err != nil {
		return nil, err
	}

	groups, err := groupFiles(files)

	if err != nil {
		return nil, err
	}

	limits := m.settings().Limits()

	var total int64

	for _, group := range groups {
		if !plainname.Valid(group.name) || !actor.Writer && !userSkinNamePattern.MatchString(group.name) {
			return nil, refuse(ErrInvalidSkin, i18n.Msg("text.skins.invalid_name", "invalid skin upload: \"{name}\" is not a valid skin name (letters, digits, space, _ . - only)", "name", group.name))
		}

		if len(group.files) > limits.MaxFiles {
			return nil, refuse(ErrTooManyFiles, i18n.Msg("text.skins.too_many_files_limit", "the skin has too many files ({files} files, at most {max})", "files", len(group.files), "max", limits.MaxFiles))
		}

		if group.size > limits.MaxSkinBytes {
			return nil, refuse(ErrSkinTooLarge, i18n.Msg("text.skins.too_large_limit", "the skin is too large ({size} MB, at most {max} MB)", "size", group.size>>20, "max", limits.MaxSkinBytes>>20))
		}

		if !actor.Writer {
			for _, file := range group.files {
				if !userFileExtensions[strings.ToLower(path.Ext(file.Path))] {
					return nil, refuse(ErrInvalidSkin, i18n.Msg("text.skins.file_type", "invalid skin upload: files of type \"{type}\" are not allowed in skins", "type", path.Ext(file.Path)))
				}
			}
		}

		total += group.size
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	existing, err := m.store.ListUserSkins()

	if err != nil {
		return nil, err
	}

	// skins that this upload replaces do not count twice
	var replaced int64

	folders := make([]string, len(groups))

	for i, group := range groups {
		folders[i] = group.name

		if !actor.Writer || vis != smmodel.SkinGlobal {
			folders[i] = smmodel.UserSkinFolder(actor.ID, group.name)
		}

		for _, skin := range existing {
			if skin.Car == car && skin.Folder == folders[i] {
				if skin.Owner != actor.ID && !actor.Writer {
					return nil, ErrNameTaken
				}

				if skin.Owner == actor.ID {
					replaced += skin.Size
				}
			}
		}
	}

	if !actor.Writer {
		if usage(existing, actor.ID, uuid.Nil)-replaced+total > limits.UserQuotaBytes {
			return nil, refuse(ErrQuotaExceeded, i18n.Msg("text.skins.quota_limit", "your skin storage is full (limit {max} MB)", "max", limits.UserQuotaBytes>>20))
		}
	}

	if err := m.CheckSpace(total); err != nil {
		return nil, err
	}

	var stored []*smmodel.UserSkin

	for i, group := range groups {
		skin, err := m.store1(actor, car, vis, team, folders[i], group, existing, limits)

		if err != nil {
			return stored, err
		}

		stored = append(stored, skin)
	}

	return stored, nil
}

// store1 writes one skin: into a staging directory first (counting the real bytes), then moved into place.
func (m *Manager) store1(actor Actor, car string, vis smmodel.SkinVisibility, team uuid.UUID, folder string, group *uploadGroup, existing []*smmodel.UserSkin, limits smmodel.SkinLimits) (*smmodel.UserSkin, error) {
	stagingRoot := filepath.Join(m.installPath(), "content", "user-skins", ".staging")

	if err := os.MkdirAll(stagingRoot, 0755); err != nil {
		return nil, err
	}

	staging, err := ioutil.TempDir(stagingRoot, "skin-")

	if err != nil {
		return nil, err
	}

	defer os.RemoveAll(staging)

	var written int64

	for _, file := range group.files {
		n, err := writeFile(staging, file, limits.MaxSkinBytes-written)

		if err != nil {
			return nil, err
		}

		written += n
	}

	skin := smmodel.NewUserSkin(car, group.name, actor.ID, len(group.files), written, m.now())
	skin.Folder = folder

	var old *smmodel.UserSkin

	for _, e := range existing {
		if e.Car == car && e.Folder == folder {
			old = e
		}
	}

	if old != nil {
		// keep identity and owner of the skin that is replaced
		skin.ID, skin.Owner, skin.Created = old.ID, old.Owner, old.Created
	}

	if err := skin.SetVisibility(vis, team, m.now()); err != nil {
		return nil, err
	}

	dest := m.Dir(skin)

	if old != nil {
		if err := os.RemoveAll(m.Dir(old)); err != nil {
			return nil, err
		}
	} else if _, err := os.Lstat(dest); err == nil && !actor.Writer {
		// a directory without a record: placed by hand, not ours to replace
		return nil, ErrNameTaken
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return nil, err
	}

	// a global upload of an admin also replaces a skin that was placed by hand
	if err := os.RemoveAll(dest); err != nil {
		return nil, err
	}

	if err := os.Rename(staging, dest); err != nil {
		return nil, err
	}

	if err := m.store.UpsertUserSkin(skin); err != nil {
		os.RemoveAll(dest)

		return nil, err
	}

	return skin, nil
}

// writeFile copies one upload file below dir and returns the bytes written. It stops at limit bytes.
func writeFile(dir string, file File, limit int64) (int64, error) {
	if limit < 0 {
		limit = 0
	}

	target := filepath.Join(dir, filepath.FromSlash(file.Path))

	if rel, err := filepath.Rel(dir, target); err != nil || strings.HasPrefix(rel, "..") {
		return 0, refuse(ErrInvalidSkin, i18n.Msg("text.skins.bad_file_name", "invalid skin upload: bad file name"))
	}

	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return 0, err
	}

	r, err := file.Open()

	if err != nil {
		return 0, err
	}

	defer r.Close()

	w, err := os.Create(target)

	if err != nil {
		return 0, err
	}

	n, err := io.Copy(w, io.LimitReader(r, limit+1))

	if closeErr := w.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return 0, err
	}

	if n > limit {
		return 0, ErrSkinTooLarge
	}

	return n, nil
}

// SetVisibility changes who can use a skin and moves its files accordingly. Only the owner (or an account with write
// access) may do it, an owner without write access needs the global sharing option to make a skin global.
func (m *Manager) SetVisibility(actor Actor, id string, vis smmodel.SkinVisibility, team uuid.UUID) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	id, err := canonicalID(id)

	if err != nil {
		return err
	}

	skin, err := m.store.FindUserSkinByID(id)

	if err != nil {
		return err
	}

	if actor.ID == uuid.Nil || skin.Owner != actor.ID && !actor.Writer {
		return ErrNotAllowed
	}

	// whoever may change the skin can always make it private; sharing follows the upload rules
	if vis != smmodel.SkinPrivate {
		if err := m.authorize(actor, vis, team); err != nil {
			return err
		}
	}

	from := m.Dir(skin)
	moved := *skin

	if err := moved.SetVisibility(vis, team, m.now()); err != nil {
		return err
	}

	to := m.Dir(&moved)

	if from != to {
		if _, err := os.Lstat(to); err == nil {
			return ErrNameTaken
		}

		if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
			return err
		}

		if err := os.Rename(from, to); err != nil {
			return err
		}
	}

	if err := m.store.UpsertUserSkin(&moved); err != nil {
		if from != to {
			_ = os.Rename(to, from)
		}

		return err
	}

	return nil
}

// Delete removes a skin with its files. Only the owner or an account with write access may do it.
func (m *Manager) Delete(actor Actor, id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	id, err := canonicalID(id)

	if err != nil {
		return err
	}

	skin, err := m.store.FindUserSkinByID(id)

	if err != nil {
		return err
	}

	if actor.ID == uuid.Nil || skin.Owner != actor.ID && !actor.Writer {
		return ErrNotAllowed
	}

	if err := os.RemoveAll(m.Dir(skin)); err != nil {
		return err
	}

	return m.store.DeleteUserSkin(id)
}

// ForgetFolder drops the record of a global skin whose directory is deleted by other means (the admin delete of a
// skin by name).
func (m *Manager) ForgetFolder(car, folder string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	skin, err := m.ByFolder(car, folder)

	if err != nil || skin == nil || skin.Normalized() != smmodel.SkinGlobal {
		return err
	}

	return m.store.DeleteUserSkin(skin.ID.String())
}

// UsableSkins returns, per car, the folder names of the private and team skins that the account can use (its own and
// those of its teams). Global skins are not listed: they are in the skins directory of the car. A zero id has none.
func (m *Manager) UsableSkins(account uuid.UUID) (map[string][]string, error) {
	result := make(map[string][]string)

	if account == uuid.Nil {
		return result, nil
	}

	skins, err := m.store.ListUserSkins()

	if err != nil {
		return nil, err
	}

	teams := m.teams(account)

	for _, skin := range skins {
		if skin.Normalized() != smmodel.SkinGlobal && skin.UsableBy(account, teams) {
			result[skin.Car] = append(result[skin.Car], skin.Folder)
		}
	}

	return result, nil
}

func (m *Manager) teams(account uuid.UUID) []uuid.UUID {
	if m.teamsOf == nil {
		return nil
	}

	return m.teamsOf(account)
}

// CheckUsable returns ErrNotAllowed if the skin is a private or team skin that the account may not use. Skins
// without a record (global skins, skins placed by hand, random or empty skins) are fine.
func (m *Manager) CheckUsable(car, skin string, account uuid.UUID) error {
	if skin == "" || !plainname.Valid(skin) {
		return nil
	}

	record, err := m.ByFolder(car, skin)

	if err != nil || record == nil {
		return err
	}

	if !record.UsableBy(account, m.teams(account)) {
		return ErrNotAllowed
	}

	return nil
}

// CanAccess reports whether the actor may see and download the skin: whoever can use it, and accounts with write access.
func (m *Manager) CanAccess(skin *smmodel.UserSkin, actor Actor) bool {
	return actor.Writer && actor.ID != uuid.Nil || skin.UsableBy(actor.ID, m.teams(actor.ID))
}

// Resolve finds the directory of a skin that is not in the skins directory of the car: a private or team skin. It
// returns nil if the car has no such skin (or its files are gone).
func (m *Manager) Resolve(car, folder string) (*smmodel.UserSkin, string, error) {
	if !plainname.Valid(car) || !plainname.Valid(folder) {
		return nil, "", nil
	}

	skin, err := m.ByFolder(car, folder)

	if err != nil || skin == nil || skin.Normalized() == smmodel.SkinGlobal {
		return nil, "", err
	}

	dir := m.Dir(skin)

	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil, "", nil
	}

	return skin, dir, nil
}

// ListForCar returns all skin records of a car: the accounts' own skins, those of their teams and the global ones.
// Accounts with write access see everything.
func (m *Manager) ListForCar(car string, actor Actor) ([]*smmodel.UserSkin, error) {
	skins, err := m.store.ListUserSkins()

	if err != nil {
		return nil, err
	}

	var result []*smmodel.UserSkin

	for _, skin := range skins {
		if skin.Car == car && (skin.Normalized() == smmodel.SkinGlobal || m.CanAccess(skin, actor)) {
			result = append(result, skin)
		}
	}

	return result, nil
}

// Settings returns the current skin options.
func (m *Manager) Settings() smmodel.SkinSettings {
	return m.settings()
}

// CanUpload reports why an actor cannot upload skins at all, nil if they can.
func (m *Manager) CanUpload(actor Actor) error {
	if actor.ID == uuid.Nil {
		return ErrNotAllowed
	}

	if !actor.Writer && !m.settings().UserUploads {
		return ErrUploadsDisabled
	}

	return nil
}

// CanShareGlobal reports whether the actor may make skins global.
func (m *Manager) CanShareGlobal(actor Actor) bool {
	return actor.ID != uuid.Nil && (actor.Writer || m.settings().GlobalShare && m.settings().UserUploads)
}

// UploadCap is the most bytes of one upload request the server reads for the actor: what the limits allow, plus room
// for the form.
func (m *Manager) UploadCap(actor Actor) int64 {
	limits := m.settings().Limits()
	capacity := limits.MaxSkinBytes * maxSkinsPerUpload

	if !actor.Writer && limits.UserQuotaBytes < capacity {
		capacity = limits.UserQuotaBytes
	}

	return capacity + 1<<20
}

// Quota returns how many bytes the actor uses, and how many they may use (0 if there is no quota: writers).
func (m *Manager) Quota(actor Actor) (used, quota int64, err error) {
	used, err = m.Usage(actor.ID)

	if err != nil || actor.Writer {
		return used, 0, err
	}

	return used, m.settings().Limits().UserQuotaBytes, nil
}

// refusal is a refusal with details: errors.Is finds its sentinel error, the UI shows (and translates) its message.
type refusal struct {
	err error
	msg i18n.Message
}

func refuse(err error, msg i18n.Message) error {
	return &refusal{err: err, msg: msg}
}

func (r *refusal) Error() string {
	return "skins: " + r.msg.String()
}

func (r *refusal) Unwrap() error {
	return r.err
}

// I18nMessage returns the message for the user (see i18n.Translatable).
func (r *refusal) I18nMessage() i18n.Message {
	return r.msg
}

// errorMessages are the texts for the user of the errors without details.
var errorMessages = []struct {
	err error
	msg i18n.Message
}{
	{ErrUploadsDisabled, i18n.Msg("text.skins.uploads_disabled", "skin uploads by users are disabled")},
	{ErrGlobalDisabled, i18n.Msg("text.skins.global_disabled", "sharing skins globally is disabled")},
	{ErrNotAllowed, i18n.Msg("text.skins.not_allowed", "you are not allowed to change this skin")},
	{ErrNotTeamMember, i18n.Msg("text.skins.not_team_member", "you are not a member of this team")},
	{ErrInvalidSkin, i18n.Msg("text.skins.invalid", "invalid skin upload")},
	{ErrTooManyFiles, i18n.Msg("text.skins.too_many_files", "the skin has too many files")},
	{ErrSkinTooLarge, i18n.Msg("text.skins.too_large", "the skin is too large")},
	{ErrQuotaExceeded, i18n.Msg("text.skins.quota_exceeded", "your skin storage is full")},
	{ErrDiskFull, i18n.Msg("text.skins.disk_full", "not enough free disk space on the server")},
	{ErrNameTaken, i18n.Msg("text.skins.name_taken", "a skin with this name exists already")},
	{smmodel.ErrSkinVisibility, i18n.Msg("text.skins.invalid_visibility", "invalid skin visibility")},
	{smmodel.ErrUserSkinNotFound, i18n.Msg("text.skins.not_found", "skin not found")},
}

// Message turns an error of the manager into a message for the user (without the package prefix).
func Message(err error) i18n.Message {
	var translatable i18n.Translatable

	if errors.As(err, &translatable) {
		return translatable.I18nMessage()
	}

	for _, e := range errorMessages {
		if errors.Is(err, e.err) {
			return e.msg
		}
	}

	return i18n.Message{English: strings.TrimPrefix(err.Error(), "skins: ")}
}

// IsUserError tells whether err is a refusal the user can understand and fix (as opposed to a failure of the server).
func IsUserError(err error) bool {
	for _, e := range []error{
		ErrUploadsDisabled, ErrGlobalDisabled, ErrNotAllowed, ErrNotTeamMember, ErrInvalidSkin, ErrTooManyFiles,
		ErrSkinTooLarge, ErrQuotaExceeded, ErrDiskFull, ErrNameTaken, smmodel.ErrSkinVisibility, smmodel.ErrUserSkinNotFound,
	} {
		if errors.Is(err, e) {
			return true
		}
	}

	return false
}

// canonicalID turns the id of a skin from the outside into the form the stores use. The stores build file names from
// ids, so only real UUIDs get through.
func canonicalID(id string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(id))

	if err != nil {
		return "", smmodel.ErrUserSkinNotFound
	}

	return parsed.String(), nil
}

// ForgetCar drops the records and the private and team skins of a car that is deleted. Global skins go with the car
// directory itself.
func (m *Manager) ForgetCar(car string) error {
	if !plainname.Valid(car) {
		return nil
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	skins, err := m.store.ListUserSkins()

	if err != nil {
		return err
	}

	for _, skin := range skins {
		if skin.Car == car {
			if err := m.store.DeleteUserSkin(skin.ID.String()); err != nil {
				return err
			}
		}
	}

	return os.RemoveAll(m.UserDir(car))
}
