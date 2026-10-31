package web

import (
	"html/template"
	"net/http"

	"github.com/go-chi/chi"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

type TemplateVars interface {
	Get() *BaseTemplateVars
}

type BaseTemplateVars struct {
	Messages         []interface{}
	Errors           []interface{}
	ServerStatus     bool
	ServerEvent      smmodel.RaceEvent
	ServerName       string
	CustomCSS        template.CSS
	User             *smmodel.Account
	IsDarkTheme      bool
	Request          *http.Request
	Debug            bool
	RecaptchaSiteKey string
	WideContainer    bool
	OGImage          string
	ServerID         smmodel.ServerID

	// CurrentServer is the server slot the page is for, Servers lists all slots.
	CurrentServer ServerRef
	Servers       []ServerRef

	ShowEventDetailsPopup bool
	ShowUpgradePopup      bool
}

func (b *BaseTemplateVars) Get() *BaseTemplateVars {
	return b
}

func (tr *Renderer) addData(w http.ResponseWriter, r *http.Request, vars TemplateVars) error {
	session := Session(r)

	data := vars.Get()

	if flashes := session.Flashes(); len(flashes) > 0 {
		data.Messages = flashes
	}

	errSession := errorSession(r)

	if flashes := errSession.Flashes(); len(flashes) > 0 {
		data.Errors = flashes
	}

	_ = session.Save(r, w)
	_ = errSession.Save(r, w)

	server, err := tr.opts.ServerFor(r)

	if err != nil {
		return err
	}

	opts, err := server.Store.LoadServerOptions()

	if err != nil {
		return err
	}

	data.CurrentServer = server.ServerRef
	data.Servers = tr.opts.Servers()
	data.ServerStatus = server.State.IsRunning()
	data.ServerEvent = server.State.Event()
	data.ServerName = opts.Name
	data.CustomCSS = template.CSS(opts.CustomCSS)
	data.User = AccountFromRequest(r)
	data.IsDarkTheme = opts.DarkTheme == 1
	data.Request = r
	data.Debug = tr.opts.Debug
	data.RecaptchaSiteKey = tr.opts.Config.Championships.RecaptchaConfig.SiteKey
	data.ServerID = ScopeFromRequest(r).ServerID
	data.ShowEventDetailsPopup = opts.ShowEventDetailsPopup
	data.ShowUpgradePopup = data.User.ShouldSeeUpgradePopup(tr.opts.BuildVersion, tr.opts.Config.Accounts.AdminPasswordOverride)

	data.OGImage = opts.OGImage

	if id := chi.URLParam(r, "championshipID"); id != "" {
		championship, err := tr.opts.Store.LoadChampionship(id)

		if err == nil && championship.OGImage != "" {
			data.OGImage = championship.OGImage
		}
	}

	return nil
}
