package web

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/Masterminds/sprig"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
)

// ServerState is what the renderer needs to know about the running Assetto Corsa server.
type ServerState interface {
	IsRunning() bool
	Event() smmodel.RaceEvent
}

// ServerContext is what the renderer needs to know about the server slot a request is for.
type ServerContext struct {
	ServerRef

	// Store is the view of the store for the slot (see smstore.ForServer).
	Store smstore.Store
	State ServerState
}

// RendererOptions configure a Renderer.
type RendererOptions struct {
	Config *smconfig.Configuration
	Store  smstore.Store
	Loader TemplateLoader

	// ServerFor returns the server slot the request is for, Servers lists all slots.
	ServerFor func(r *http.Request) (*ServerContext, error)
	Servers   func() []ServerRef

	// BuildVersion is shown in the UI and used to bust asset caches.
	BuildVersion string
	Debug        bool

	// Reload makes the renderer reload the templates on every request.
	Reload bool

	// Funcs are template functions which depend on the rest of the server manager. They are added to (and may replace)
	// the functions the renderer provides itself.
	Funcs template.FuncMap
}

// Renderer is the template engine.
type Renderer struct {
	opts RendererOptions

	templates map[string]*template.Template

	mutex sync.Mutex
}

func NewRenderer(opts RendererOptions) (*Renderer, error) {
	tr := &Renderer{
		opts: opts,

		templates: make(map[string]*template.Template),
	}

	err := tr.init()

	if err != nil {
		return nil, err
	}

	return tr, nil
}

// dummy access func is used in place of read/write/admin funcs when initialising templates
func dummyAccessFunc() bool {
	return false
}

// init loads template files into memory.
func (tr *Renderer) init() error {
	tr.mutex.Lock()
	defer tr.mutex.Unlock()

	err := tr.opts.Loader.Init()

	if err != nil {
		return err
	}

	funcs := sprig.FuncMap()
	funcs["htmlAttr"] = func(s string) template.HTMLAttr {
		return template.HTMLAttr(s)
	}
	funcs["ordinal"] = smmodel.Ordinal
	funcs["prettify"] = smcontent.PrettifyName
	funcs["jsonEncode"] = jsonEncode
	funcs["timeFormat"] = timeFormat
	funcs["dateFormat"] = dateFormat
	funcs["timeZone"] = timeZone
	funcs["hourAndZone"] = hourAndZoneFormat
	funcs["localFormatHourAndZone"] = localFormatHelperHourAndZone
	funcs["addTime"] = addTime
	funcs["isBefore"] = isBefore
	funcs["multiplyFloats"] = multiplyFloats
	funcs["percentage"] = percentage
	funcs["stripGeotagCrap"] = stripGeotagCrap
	funcs["ReadAccess"] = dummyAccessFunc
	funcs["WriteAccess"] = dummyAccessFunc
	funcs["DeleteAccess"] = dummyAccessFunc
	funcs["AdminAccess"] = dummyAccessFunc
	funcs["LoggedIn"] = dummyAccessFunc
	funcs["StatsAccess"] = dummyAccessFunc
	funcs["serverURL"] = func(path string) string { return path }
	funcs["serverName"] = tr.serverName
	funcs["stringArrayToCSV"] = stringArrayToCSV
	funcs["dict"] = templateDict
	funcs["asset"] = NewAssetHelper("/", "", "", map[string]string{"cb": tr.opts.BuildVersion}).GetURL
	funcs["SessionType"] = func(s string) serverconfig.SessionType { return serverconfig.SessionType(s) }
	funcs["Config"] = func() *smconfig.Configuration { return tr.opts.Config }
	funcs["Version"] = func() string { return tr.opts.BuildVersion }
	funcs["fullTimeFormat"] = fullTimeFormat
	funcs["localFormat"] = localFormatHelper
	funcs["driverName"] = smresults.DriverName
	funcs["driverURL"] = DriverURL
	funcs["entrantNames"] = func(e serverconfig.EntryList) string { return e.Entrants(smresults.DriverName) }
	funcs["shortenTrackName"] = shortenTrackName
	funcs["trustHTML"] = func(s string) template.HTML {
		return template.HTML(s)
	}
	funcs["formatDuration"] = formatDuration
	funcs["appendQuery"] = appendQuery
	funcs["yn"] = func(b bool) string {
		if b {
			return "Yes"
		}

		return "No"
	}
	funcs["trackMapURL"] = smcontent.TrackMapImageURL
	funcs["sunAngleToTimeOfDay"] = sunAngleToTimeOfDay
	funcs["anonymiseDriverGUID"] = smresults.AnonymiseDriverGUID

	for name, f := range tr.opts.Funcs {
		funcs[name] = f
	}

	tr.templates, err = tr.opts.Loader.Templates(funcs)

	if err != nil {
		return err
	}

	return nil
}

// LoadTemplate reads a template from templates and renders it with data to the given io.Writer
func (tr *Renderer) LoadTemplate(w http.ResponseWriter, r *http.Request, view string, vars TemplateVars) error {
	if tr.opts.Reload {
		// reload templates on every request if enabled, so
		// that we don't have to constantly restart the website
		err := tr.init()

		if err != nil {
			return err
		}
	}

	t, ok := tr.templates[filepath.ToSlash(view)]

	if !ok {
		return fmt.Errorf("unable to find template: %s", filepath.ToSlash(view))
	}

	if vars == nil {
		vars = &BaseTemplateVars{}
	}

	if err := tr.addData(w, r, vars); err != nil {
		return err
	}

	t.Funcs(map[string]interface{}{
		"ReadAccess":   ReadAccess(r),
		"WriteAccess":  WriteAccess(r),
		"DeleteAccess": DeleteAccess(r),
		"AdminAccess":  AdminAccess(r),
		"LoggedIn":     LoggedIn(r),
		"StatsAccess":  StatsAccess(r),
		"driverURL":    func(guid string) string { return statsDriverURL(r, guid) },
		"serverURL":    func(path string) string { return ServerURL(r, path) },
	})

	return t.ExecuteTemplate(w, "base", vars)
}

// MustLoadTemplate asserts that a LoadTemplate call must succeed or be dealt with via the http.ResponseWriter
func (tr *Renderer) MustLoadTemplate(w http.ResponseWriter, r *http.Request, view string, vars TemplateVars) {
	err := tr.LoadTemplate(w, r, view, vars)

	if err != nil {
		logrus.WithError(err).Errorf("Unable to load template: %s", view)
		http.Error(w, "unable to load template", http.StatusInternalServerError)
		return
	}
}

func (tr *Renderer) LoadPartial(w http.ResponseWriter, r *http.Request, partial string, vars TemplateVars) error {
	if tr.opts.Reload {
		// reload templates on every request if enabled, so
		// that we don't have to constantly restart the website
		err := tr.init()

		if err != nil {
			return err
		}
	}

	t, ok := tr.templates[filepath.ToSlash(partial)]

	if !ok {
		return errors.New("partial not found")
	}

	if vars == nil {
		vars = &BaseTemplateVars{}
	}

	if err := tr.addData(w, r, vars); err != nil {
		return err
	}

	t.Funcs(map[string]interface{}{
		"ReadAccess":   ReadAccess(r),
		"WriteAccess":  WriteAccess(r),
		"DeleteAccess": DeleteAccess(r),
		"AdminAccess":  AdminAccess(r),
		"LoggedIn":     LoggedIn(r),
		"StatsAccess":  StatsAccess(r),
		"driverURL":    func(guid string) string { return statsDriverURL(r, guid) },
		"serverURL":    func(path string) string { return ServerURL(r, path) },
	})

	return t.ExecuteTemplate(w, "partial", vars)
}

func (tr *Renderer) MustLoadPartial(w http.ResponseWriter, r *http.Request, partial string, vars TemplateVars) {
	err := tr.LoadPartial(w, r, partial, vars)

	if err != nil {
		logrus.WithError(err).Errorf("Unable to load partial: %s", partial)
		http.Error(w, "unable to load partial", http.StatusInternalServerError)
		return
	}
}

// serverName returns the name of the server slot with the given id, or an empty string if it is not a slot (e.g. the
// event was scheduled for a server which has since been removed from the configuration).
func (tr *Renderer) serverName(id smmodel.ServerID) string {
	if tr.opts.Servers == nil {
		return ""
	}

	for _, server := range tr.opts.Servers() {
		if server.ID == id {
			return server.Name
		}
	}

	return ""
}
