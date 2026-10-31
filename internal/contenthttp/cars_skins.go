package contenthttp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/plainname"
	"github.com/wernersbacher/ACPitwall/internal/skins"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// SkinInfo gives the skin pages the names they show. The resolver implements it with the teams and accounts.
type SkinInfo interface {
	// TeamsOf returns the teams an account is a member of.
	TeamsOf(account uuid.UUID) []*smmodel.Team
	// TeamName returns the name of a team, "" if it is unknown.
	TeamName(id uuid.UUID) string
	// AccountName returns the name to show for an account, "" if it is unknown.
	AccountName(id uuid.UUID) string
}

// SetSkinInfo sets where the names shown on the skin pages come from.
func (ch *CarsHandler) SetSkinInfo(info SkinInfo) {
	ch.skinInfo = info
}

// SkinRow is a skin on the car page.
type SkinRow struct {
	Skin      *smmodel.UserSkin
	OwnerName string
	TeamName  string

	// Manage is true if the visitor may change and delete the skin.
	Manage bool
	// CanGlobal tells whether the skin can be made global by the visitor.
	CanGlobal bool

	PreviewURL  string
	DownloadURL string
}

// CarSkins is the skin section of the car page: what the visitor may do and the skins of users they can see.
type CarSkins struct {
	CanUpload bool
	CanGlobal bool

	// DefaultVisibility is preselected in the upload form: global for accounts with write access, private for others.
	DefaultVisibility string
	Teams             []*smmodel.Team

	Rows []SkinRow

	// UsedMB and QuotaMB tell how much of their storage the visitor uses; QuotaMB is 0 without a quota.
	UsedMB, QuotaMB int64
	MaxSkinMB       int64
	MaxFiles        int
}

// carSkins builds the skin section of the car page, nil if the skins of users are not available.
func (ch *CarsHandler) carSkins(r *http.Request, car string) (*CarSkins, error) {
	if ch.userSkins == nil {
		return nil, nil
	}

	actor := smweb.SkinActorFromRequest(r)
	limits := ch.userSkins.Settings().Limits()

	result := &CarSkins{
		CanUpload:         ch.userSkins.CanUpload(actor) == nil,
		CanGlobal:         ch.userSkins.CanShareGlobal(actor),
		DefaultVisibility: string(smmodel.SkinPrivate),
		MaxSkinMB:         limits.MaxSkinBytes >> 20,
		MaxFiles:          limits.MaxFiles,
	}

	if actor.Writer {
		result.DefaultVisibility = string(smmodel.SkinGlobal)
	}

	if ch.skinInfo != nil && actor.ID != uuid.Nil {
		result.Teams = ch.skinInfo.TeamsOf(actor.ID)
	}

	used, quota, err := ch.userSkins.Quota(actor)

	if err != nil {
		return nil, err
	}

	result.UsedMB, result.QuotaMB = used>>20, quota>>20

	records, err := ch.userSkins.ListForCar(car, actor)

	if err != nil {
		return nil, err
	}

	for _, skin := range records {
		row := SkinRow{
			Skin:        skin,
			Manage:      actor.ID != uuid.Nil && (skin.Owner == actor.ID || actor.Writer),
			CanGlobal:   result.CanGlobal,
			DownloadURL: "/car/" + car + "/skin/" + skin.Folder + "/download",
		}

		if skin.Normalized() == smmodel.SkinGlobal {
			row.PreviewURL = "/content/cars/" + car + "/skins/" + skin.Folder + "/preview.jpg"
		} else {
			row.PreviewURL = "/car/" + car + "/skin/" + skin.Folder + "/preview.jpg"
		}

		if ch.skinInfo != nil {
			row.OwnerName = ch.skinInfo.AccountName(skin.Owner)

			if skin.TeamID != uuid.Nil {
				row.TeamName = ch.skinInfo.TeamName(skin.TeamID)
			}
		}

		result.Rows = append(result.Rows, row)
	}

	return result, nil
}

// skinBack redirects to the page the form came from, or the car page.
func skinBack(w http.ResponseWriter, r *http.Request, car string) {
	target := r.Referer()

	if target == "" {
		target = "/car/" + car
	}

	http.Redirect(w, r, target, http.StatusFound)
}

// skinFailed reports an error of the skin manager: refusals as a message to the user, everything else as an error.
func skinFailed(w http.ResponseWriter, r *http.Request, car string, err error, what string) {
	if skins.IsUserError(err) {
		smweb.AddErrorFlash(w, r, strings.ToUpper(what[:1])+what[1:]+" failed: "+skins.Message(err)+".")
		skinBack(w, r, car)

		return
	}

	logrus.WithError(err).Errorf("could not %s for %s", what, car)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// skinActor returns the actor of a request or answers that a login is needed.
func skinActor(w http.ResponseWriter, r *http.Request, car string) (skins.Actor, bool) {
	actor := smweb.SkinActorFromRequest(r)

	if actor.ID == uuid.Nil {
		smweb.AddErrorFlash(w, r, "Please log in to manage skins.")
		skinBack(w, r, car)

		return actor, false
	}

	return actor, true
}

// UploadSkin stores the skins of an upload for the car. Every logged in account may upload if the server allows it;
// accounts with write access upload global skins by default. The form fields "visibility" (private, team or global)
// and "team" (a team id) choose who can use the skins.
func (ch *CarsHandler) UploadSkin(w http.ResponseWriter, r *http.Request) {
	car := chi.URLParam(r, "name")

	if !plainname.Valid(car) || ch.userSkins == nil {
		http.NotFound(w, r)
		return
	}

	actor, ok := skinActor(w, r, car)

	if !ok {
		return
	}

	if err := ch.userSkins.CanUpload(actor); err != nil {
		skinFailed(w, r, car, err, "skin upload")
		return
	}

	// refuse before reading the body if it cannot fit
	if err := ch.userSkins.CheckSpace(r.ContentLength); err != nil {
		skinFailed(w, r, car, err, "skin upload")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, ch.userSkins.UploadCap(actor))

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		smweb.AddErrorFlash(w, r, "The skin upload was refused: it is larger than the skin limits allow.")
		skinBack(w, r, car)

		return
	}

	defer r.MultipartForm.RemoveAll()

	visibility := smmodel.SkinPrivate

	if actor.Writer {
		visibility = smmodel.SkinGlobal
	}

	if value := r.FormValue("visibility"); value != "" {
		var err error

		if visibility, err = smmodel.ParseSkinVisibility(value); err != nil {
			skinFailed(w, r, car, err, "skin upload")
			return
		}
	}

	team, _ := uuid.Parse(strings.TrimSpace(r.FormValue("team")))

	stored, err := ch.userSkins.Upload(actor, car, visibility, team, skins.FilesFromMultipart(r.MultipartForm.File))

	if err != nil {
		skinFailed(w, r, car, err, "skin upload")
		return
	}

	names := make([]string, 0, len(stored))

	for _, skin := range stored {
		names = append(names, skin.Name)
	}

	smweb.AddFlash(w, r, fmt.Sprintf("Skin %s uploaded successfully (%s).", strings.Join(names, ", "), visibility))
	skinBack(w, r, car)
}

// SetSkinVisibility changes who can use one of the skins of the visitor: the form fields are "skin" (the id),
// "visibility" and, for team skins, "team".
func (ch *CarsHandler) SetSkinVisibility(w http.ResponseWriter, r *http.Request) {
	car := chi.URLParam(r, "name")

	if !plainname.Valid(car) || ch.userSkins == nil {
		http.NotFound(w, r)
		return
	}

	actor, ok := skinActor(w, r, car)

	if !ok {
		return
	}

	visibility, err := smmodel.ParseSkinVisibility(r.FormValue("visibility"))

	if err != nil {
		skinFailed(w, r, car, err, "changing the skin")
		return
	}

	team, _ := uuid.Parse(strings.TrimSpace(r.FormValue("team")))

	if err := ch.userSkins.SetVisibility(actor, r.FormValue("skin"), visibility, team); err != nil {
		skinFailed(w, r, car, err, "changing the skin")
		return
	}

	smweb.AddFlash(w, r, "The skin is "+string(visibility)+" now.")
	skinBack(w, r, car)
}

// RemoveSkin deletes one of the skins of the visitor (or any skin, for accounts with write access): the form field
// "skin" is the id.
func (ch *CarsHandler) RemoveSkin(w http.ResponseWriter, r *http.Request) {
	car := chi.URLParam(r, "name")

	if !plainname.Valid(car) || ch.userSkins == nil {
		http.NotFound(w, r)
		return
	}

	actor, ok := skinActor(w, r, car)

	if !ok {
		return
	}

	if err := ch.userSkins.Delete(actor, r.FormValue("skin")); err != nil {
		skinFailed(w, r, car, err, "deleting the skin")
		return
	}

	smweb.AddFlash(w, r, "Skin deleted successfully!")
	skinBack(w, r, car)
}

// SizeLabel is the size of the skin for people, like "2.4 MB".
func (r SkinRow) SizeLabel() string {
	switch size := r.Skin.Size; {
	case size >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(size)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", size)
	}
}
