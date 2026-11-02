package web

import (
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/Masterminds/sprig"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
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

	// Funcs are template functions which depend on the rest of ACPitwall. They are added to (and may replace)
	// the functions the renderer provides itself.
	Funcs template.FuncMap
}

// Renderer is the template engine.
type Renderer struct {
	opts RendererOptions

	// templates holds the templates of every language by language code and view, each set with the translation
	// functions of its language bound, because a template's functions can't be changed safely per request.
	templates map[string]map[string]*viewTemplate
	bundle    *i18n.Bundle

	// fingerprint is that of the template files (see ChangeDetector) the templates were loaded from.
	fingerprint string

	mutex sync.Mutex
}

func NewRenderer(opts RendererOptions) (*Renderer, error) {
	tr := &Renderer{
		opts: opts,

		templates: make(map[string]map[string]*viewTemplate),
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

	// taken before reading the files, so that a change made while loading is noticed by the next request
	fingerprint, _ := tr.fingerprintOfFiles()

	err := tr.opts.Loader.Init()

	if err != nil {
		return err
	}

	funcs := sprig.FuncMap()
	funcs["htmlAttr"] = func(s string) template.HTMLAttr {
		return template.HTMLAttr(s)
	}

	funcs["prettify"] = smcontent.PrettifyName
	funcs["jsonEncode"] = jsonEncode
	funcs["timeZone"] = timeZone
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
	funcs["driverName"] = smresults.DriverName
	funcs["driverURL"] = DriverURL
	funcs["entrantNames"] = func(e serverconfig.EntryList) string { return e.Entrants(smresults.DriverName) }
	funcs["shortenTrackName"] = shortenTrackName
	funcs["trustHTML"] = func(s string) template.HTML {
		return template.HTML(s)
	}
	funcs["formatDuration"] = formatDuration
	funcs["appendQuery"] = appendQuery

	funcs["trackMapURL"] = smcontent.TrackMapImageURL
	funcs["sunAngleToTimeOfDay"] = sunAngleToTimeOfDay
	funcs["anonymiseDriverGUID"] = smresults.AnonymiseDriverGUID

	for name, f := range tr.opts.Funcs {
		funcs[name] = f
	}

	locales, err := tr.opts.Loader.Locales()

	if err != nil {
		return err
	}

	bundle, err := i18n.NewBundle(locales)

	if err != nil {
		return err
	}

	templates := make(map[string]map[string]*viewTemplate)

	for _, lang := range bundle.Languages() {
		for name, f := range translationFuncs(bundle, bundle.Translator(lang.Code)) {
			funcs[name] = f
		}

		parsed, err := tr.opts.Loader.Templates(funcs)

		if err != nil {
			return err
		}

		templates[lang.Code] = make(map[string]*viewTemplate, len(parsed))

		for view, t := range parsed {
			templates[lang.Code][view] = &viewTemplate{proto: t}
		}
	}

	tr.templates = templates
	tr.bundle = bundle
	tr.fingerprint = fingerprint

	return nil
}

// fingerprintOfFiles is the fingerprint of the template files, empty if the loader can't tell.
func (tr *Renderer) fingerprintOfFiles() (string, error) {
	if detector, ok := tr.opts.Loader.(ChangeDetector); ok {
		return detector.Fingerprint()
	}

	return "", nil
}

// filesChanged tells if the templates have to be reloaded: always, if the loader can't detect changes.
func (tr *Renderer) filesChanged() bool {
	current, err := tr.fingerprintOfFiles()

	if err != nil || current == "" {
		return true
	}

	tr.mutex.Lock()
	defer tr.mutex.Unlock()

	return current != tr.fingerprint
}

// Bundle returns the message catalogs.
func (tr *Renderer) Bundle() *i18n.Bundle {
	tr.mutex.Lock()
	defer tr.mutex.Unlock()

	return tr.bundle
}

// Language returns the code of the language a request is answered in: the language cookie (set by the language
// selection), else the best match of the browser's Accept-Language header, else the default language.
func (tr *Renderer) Language(r *http.Request) string {
	return RequestLanguage(r, tr.Bundle())
}

// Translator returns the translator for the language of the request.
func (tr *Renderer) Translator(r *http.Request) *i18n.Translator {
	return tr.Bundle().Translator(tr.Language(r))
}

// viewTemplate is the template of a view in one language. The parsed template is never executed itself: every
// request renders a clone of it, because the functions which depend on the request (access, server URLs) have to be
// bound to the template, and changing the functions of a template shared by concurrent requests is a data race (a page
// could be rendered with the access of another request). Clones are pooled, so that they are only escaped once.
type viewTemplate struct {
	proto *template.Template
	pool  sync.Pool
}

// get returns a template for one request, which has to be given back with put after executing it.
func (v *viewTemplate) get() (*template.Template, error) {
	if t, ok := v.pool.Get().(*template.Template); ok {
		return t, nil
	}

	return v.proto.Clone()
}

func (v *viewTemplate) put(t *template.Template) {
	v.pool.Put(t)
}

// template returns the template of the view in the language of the request.
func (tr *Renderer) template(r *http.Request, view string) (*viewTemplate, bool) {
	tr.mutex.Lock()
	templates := tr.templates
	tr.mutex.Unlock()

	t, ok := templates[tr.Language(r)][filepath.ToSlash(view)]

	return t, ok
}

// requestFuncs are the template functions which depend on the request.
func requestFuncs(r *http.Request) template.FuncMap {
	return template.FuncMap{
		"ReadAccess":   ReadAccess(r),
		"WriteAccess":  WriteAccess(r),
		"DeleteAccess": DeleteAccess(r),
		"AdminAccess":  AdminAccess(r),
		"LoggedIn":     LoggedIn(r),
		"StatsAccess":  StatsAccess(r),
		"driverURL":    func(guid string) string { return statsDriverURL(r, guid) },
		"serverURL":    func(path string) string { return ServerURL(r, path) },
	}
}

// render executes the template name ("base" or "partial") of the view for the request.
func (tr *Renderer) render(w http.ResponseWriter, r *http.Request, view, name string, vars TemplateVars) error {
	if tr.opts.Reload && tr.filesChanged() {
		// reload the templates if enabled and their files changed, so
		// that we don't have to constantly restart the website
		err := tr.init()

		if err != nil {
			return err
		}
	}

	v, ok := tr.template(r, view)

	if !ok {
		return fmt.Errorf("unable to find template: %s", filepath.ToSlash(view))
	}

	if vars == nil {
		vars = &BaseTemplateVars{}
	}

	if err := tr.addData(w, r, vars); err != nil {
		return err
	}

	t, err := v.get()

	if err != nil {
		return err
	}

	defer v.put(t)

	return t.Funcs(requestFuncs(r)).ExecuteTemplate(w, name, vars)
}

// LoadTemplate reads a template from templates and renders it with data to the given io.Writer
func (tr *Renderer) LoadTemplate(w http.ResponseWriter, r *http.Request, view string, vars TemplateVars) error {
	return tr.render(w, r, view, "base", vars)
}

// MustLoadTemplate asserts that a LoadTemplate call must succeed or be dealt with via the http.ResponseWriter
func (tr *Renderer) MustLoadTemplate(w http.ResponseWriter, r *http.Request, view string, vars TemplateVars) {
	err := tr.LoadTemplate(w, r, view, vars)

	if err != nil {
		if isClientGone(err) {
			logrus.WithError(err).Debugf("Client closed the connection while rendering template: %s", view)
			return
		}

		logrus.WithError(err).Errorf("Unable to load template: %s", view)
		http.Error(w, "unable to load template", http.StatusInternalServerError)
		return
	}
}

// isClientGone tells if rendering failed because the client went away (page left or reloaded, connection aborted or
// reset) while the response was written: nothing is wrong with the template and nobody is left to send an error to.
func isClientGone(err error) bool {
	var opErr *net.OpError

	return errors.As(err, &opErr) && opErr.Op == "write"
}

func (tr *Renderer) LoadPartial(w http.ResponseWriter, r *http.Request, partial string, vars TemplateVars) error {
	return tr.render(w, r, partial, "partial", vars)
}
func (tr *Renderer) MustLoadPartial(w http.ResponseWriter, r *http.Request, partial string, vars TemplateVars) {
	err := tr.LoadPartial(w, r, partial, vars)

	if err != nil {
		if isClientGone(err) {
			logrus.WithError(err).Debugf("Client closed the connection while rendering partial: %s", partial)
			return
		}

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
