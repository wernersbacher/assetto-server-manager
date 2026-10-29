package servermanager

import (
	smaccounts "github.com/wernersbacher/assetto-server-manager/internal/accounts"
	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/notification"
	smrace "github.com/wernersbacher/assetto-server-manager/internal/race"
	racecontrol "github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	smraceweekend "github.com/wernersbacher/assetto-server-manager/internal/raceweekend"
	"github.com/wernersbacher/assetto-server-manager/internal/schedule"
	smserver "github.com/wernersbacher/assetto-server-manager/internal/server"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"

	"net/http"

	"github.com/cj123/sessions"
	"github.com/wernersbacher/assetto-server-manager/pkg/udp"
)

type Resolver struct {
	config          *smconfig.Configuration
	store           smstore.Store
	sessionStore    sessions.Store
	templateLoader  smweb.TemplateLoader
	reloadTemplates bool

	raceManager           *smrace.RaceManager
	carManager            *smcontent.CarManager
	trackManager          *smcontent.TrackManager
	championshipManager   *smchampionship.ChampionshipManager
	accountManager        *smaccounts.AccountManager
	discordManager        *notification.DiscordManager
	notificationManager   *notification.Manager
	scheduledRacesManager *schedule.Manager
	raceWeekendManager    *smraceweekend.RaceWeekendManager

	env                   *serverenv.Env
	viewRenderer          *smweb.Renderer
	serverProcess         smserver.ServerProcess
	raceControl           *racecontrol.RaceControl
	raceControlHub        *racecontrol.RaceControlHub
	contentManagerWrapper *smserver.ContentManagerWrapper

	// handlers
	baseHandler                 *smweb.BaseHandler
	quickRaceHandler            *smrace.QuickRaceHandler
	eventDetailsHandler         *EventDetailsHandler
	customRaceHandler           *smrace.CustomRaceHandler
	championshipsHandler        *smchampionship.ChampionshipsHandler
	accountHandler              *smaccounts.AccountHandler
	auditLogHandler             *smaccounts.AuditLogHandler
	carsHandler                 *CarsHandler
	tracksHandler               *TracksHandler
	weatherHandler              *WeatherHandler
	penaltiesHandler            *racecontrol.PenaltiesHandler
	penaltiesManager            *racecontrol.PenaltiesManager
	resultsHandler              *ResultsHandler
	scheduledRacesHandler       *schedule.Handler
	contentUploadHandler        *ContentUploadHandler
	raceControlHandler          *racecontrol.RaceControlHandler
	serverAdministrationHandler *ServerAdministrationHandler
	raceWeekendHandler          *smraceweekend.RaceWeekendHandler
	strackerHandler             *StrackerHandler
	healthCheck                 *HealthCheck
	kissMyRankHandler           *KissMyRankHandler
	realPenaltyHandler          *RealPenaltyHandler
}

func NewResolver(config *smconfig.Configuration, templateLoader smweb.TemplateLoader, reloadTemplates bool, store smstore.Store) (*Resolver, error) {
	r := &Resolver{
		config:          config,
		templateLoader:  templateLoader,
		reloadTemplates: reloadTemplates,
		store:           store,
	}

	sessionStore, err := config.HTTP.CreateSessionStore()

	if err != nil {
		return nil, err
	}

	r.sessionStore = sessionStore

	if err := r.initViewRenderer(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Resolver) UDPCallback(message udp.Message) {
	if !r.config.Server.PerformanceMode {
		r.ResolveRaceControl().UDPCallback(message)
	}

	if message.Event() != udp.EventCarUpdate {
		r.resolveChampionshipManager().ChampionshipEventCallback(message)
		r.resolveRaceWeekendManager().UDPCallback(message)
		r.resolveRaceManager().LoopCallback(message)
		r.resolveContentManagerWrapper().UDPCallback(message)
	}
}

func (r *Resolver) initViewRenderer() error {
	if r.viewRenderer != nil {
		return nil
	}

	viewRenderer, err := smweb.NewRenderer(smweb.RendererOptions{
		Config:       r.config,
		Store:        r.store,
		Loader:       r.templateLoader,
		Server:       r.resolveServerProcess(),
		BuildVersion: BuildVersion,
		Debug:        Debug,
		Reload:       r.reloadTemplates,
		Funcs:        serverTemplateFuncs(),
	})

	if err != nil {
		return err
	}

	r.viewRenderer = viewRenderer

	return nil
}

// resolveEnv returns the environment of the managers.
func (r *Resolver) resolveEnv() *serverenv.Env {
	if r.env != nil {
		return r.env
	}

	r.env = globalsEnv()

	return r.env
}

func (r *Resolver) ResolveStore() smstore.Store {
	return r.store
}

func (r *Resolver) resolveServerProcess() smserver.ServerProcess {
	if r.serverProcess != nil {
		return r.serverProcess
	}

	r.serverProcess = smserver.NewAssettoServerProcess(r.config, r.UDPCallback, r.ResolveStore(), r.resolveContentManagerWrapper(), r.resolveEnv(), pluginsOutput)

	return r.serverProcess
}

func (r *Resolver) resolveContentManagerWrapper() *smserver.ContentManagerWrapper {
	if r.contentManagerWrapper != nil {
		return r.contentManagerWrapper
	}

	r.contentManagerWrapper = smserver.NewContentManagerWrapper(
		r.config,
		r.ResolveStore(),
		r.resolveCarManager(),
		r.resolveTrackManager(),
		r.resolveEnv(),
	)

	return r.contentManagerWrapper
}

func (r *Resolver) resolveRaceManager() *smrace.RaceManager {
	if r.raceManager != nil {
		return r.raceManager
	}

	r.raceManager = smrace.NewRaceManager(
		r.config,
		r.store,
		r.resolveServerProcess(),
		r.resolveCarManager(),
		r.resolveTrackManager(),
		r.resolveNotificationManager(),
		r.ResolveRaceControl(),
		r.resolveEnv(),
	)

	return r.raceManager
}

func (r *Resolver) resolveBaseHandler() *smweb.BaseHandler {
	if r.baseHandler != nil {
		return r.baseHandler
	}

	r.baseHandler = smweb.NewBaseHandler(r.viewRenderer)

	return r.baseHandler
}

func (r *Resolver) resolveCustomRaceHandler() *smrace.CustomRaceHandler {
	if r.customRaceHandler != nil {
		return r.customRaceHandler
	}

	r.customRaceHandler = smrace.NewCustomRaceHandler(
		r.config,
		r.resolveBaseHandler(),
		r.resolveRaceManager(),
		r.ResolveStore(),
	)

	return r.customRaceHandler
}

func (r *Resolver) resolveEventDetailsHandler() *EventDetailsHandler {
	if r.eventDetailsHandler != nil {
		return r.eventDetailsHandler
	}

	r.eventDetailsHandler = NewEventDetailsHandler(r.resolveBaseHandler(), r.ResolveStore(), r.resolveChampionshipManager(), r.resolveRaceWeekendManager())

	return r.eventDetailsHandler
}

func (r *Resolver) resolveAccountManager() *smaccounts.AccountManager {
	if r.accountManager != nil {
		return r.accountManager
	}

	r.accountManager = smaccounts.NewAccountManager(r.config, r.store, r.sessionStore, BuildVersion)

	return r.accountManager
}

func (r *Resolver) resolveAccountHandler() *smaccounts.AccountHandler {
	if r.accountHandler != nil {
		return r.accountHandler
	}

	r.accountHandler = smaccounts.NewAccountHandler(r.config, r.resolveBaseHandler(), r.store, r.resolveAccountManager())

	return r.accountHandler
}

func (r *Resolver) resolveQuickRaceHandler() *smrace.QuickRaceHandler {
	if r.quickRaceHandler != nil {
		return r.quickRaceHandler
	}

	r.quickRaceHandler = smrace.NewQuickRaceHandler(r.config, r.resolveBaseHandler(), r.resolveRaceManager())

	return r.quickRaceHandler
}

func (r *Resolver) resolveAuditLogHandler() *smaccounts.AuditLogHandler {
	if r.auditLogHandler != nil {
		return r.auditLogHandler
	}

	r.auditLogHandler = smaccounts.NewAuditLogHandler(r.resolveBaseHandler(), r.store)

	return r.auditLogHandler
}

func (r *Resolver) resolveCarManager() *smcontent.CarManager {
	if r.carManager != nil {
		return r.carManager
	}

	r.carManager = smcontent.NewCarManager(
		ServerInstallPath,
		r.config.Server.ScanContentFolderForChanges,
		r.config.Server.UseCarNameCache,
	)

	r.carManager.PanicCapture = func(fn func()) { panicCapture(fn) }

	return r.carManager
}

func (r *Resolver) resolveCarsHandler() *CarsHandler {
	if r.carsHandler != nil {
		return r.carsHandler
	}

	r.carsHandler = NewCarsHandler(r.resolveBaseHandler(), r.resolveCarManager(), r.resolveTrackManager())

	return r.carsHandler
}

func (r *Resolver) resolveChampionshipManager() *smchampionship.ChampionshipManager {
	if r.championshipManager != nil {
		return r.championshipManager
	}

	r.championshipManager = smchampionship.NewChampionshipManager(
		r.resolveRaceManager(),
		r.config,
		r.ResolveStore(),
		r.resolveServerProcess(),
		r.resolveEnv(),
		r.resolveNotificationManager(),
	)

	return r.championshipManager
}

func (r *Resolver) resolveChampionshipsHandler() *smchampionship.ChampionshipsHandler {
	if r.championshipsHandler != nil {
		return r.championshipsHandler
	}

	r.championshipsHandler = smchampionship.NewChampionshipsHandler(r.config, r.resolveBaseHandler(), r.resolveChampionshipManager())

	return r.championshipsHandler
}

func (r *Resolver) resolveTrackManager() *smcontent.TrackManager {
	if r.trackManager != nil {
		return r.trackManager
	}

	r.trackManager = smcontent.NewTrackManager(ServerInstallPath)

	return r.trackManager
}

func (r *Resolver) resolveTracksHandler() *TracksHandler {
	if r.tracksHandler != nil {
		return r.tracksHandler
	}

	r.tracksHandler = NewTracksHandler(r.resolveBaseHandler(), r.resolveTrackManager())

	return r.tracksHandler
}

func (r *Resolver) resolveWeatherHandler() *WeatherHandler {
	if r.weatherHandler != nil {
		return r.weatherHandler
	}

	r.weatherHandler = NewWeatherHandler(r.resolveBaseHandler())

	return r.weatherHandler
}

func (r *Resolver) resolvePenaltiesHandler() *racecontrol.PenaltiesHandler {
	if r.penaltiesHandler != nil {
		return r.penaltiesHandler
	}

	r.penaltiesHandler = racecontrol.NewPenaltiesHandler(r.resolveBaseHandler(), r.resolvePenaltiesManager())

	return r.penaltiesHandler
}

func (r *Resolver) resolvePenaltiesManager() *racecontrol.PenaltiesManager {
	if r.penaltiesHandler != nil {
		return r.penaltiesManager
	}

	r.penaltiesManager = racecontrol.NewPenaltiesManager(r.ResolveStore(), r.resolveEnv())

	return r.penaltiesManager
}

func (r *Resolver) resolveResultsHandler() *ResultsHandler {
	if r.resultsHandler != nil {
		return r.resultsHandler
	}

	r.resultsHandler = NewResultsHandler(r.resolveBaseHandler(), r.ResolveStore())

	return r.resultsHandler
}

func (r *Resolver) resolveScheduledRacesManager() *schedule.Manager {
	if r.scheduledRacesManager != nil {
		return r.scheduledRacesManager
	}

	r.scheduledRacesManager = schedule.NewManager(r.config, r.ResolveStore(), r.resolveEnv(), func(session *smmodel.RaceWeekendSession) (serverconfig.EntryList, error) {
		// resolved when called, because the race weekend manager needs the notification manager, which needs this manager
		return r.resolveRaceWeekendManager().SessionEntryList(session, session.RaceWeekend())
	})

	return r.scheduledRacesManager
}

func (r *Resolver) resolveScheduledRacesHandler() *schedule.Handler {
	if r.scheduledRacesHandler != nil {
		return r.scheduledRacesHandler
	}

	r.scheduledRacesHandler = schedule.NewHandler(r.resolveBaseHandler(), r.resolveScheduledRacesManager())

	return r.scheduledRacesHandler
}

func (r *Resolver) resolveServerAdministrationHandler() *ServerAdministrationHandler {
	if r.serverAdministrationHandler != nil {
		return r.serverAdministrationHandler
	}

	r.serverAdministrationHandler = NewServerAdministrationHandler(
		r.config,
		r.resolveBaseHandler(),
		r.ResolveStore(),
		r.resolveRaceManager(),
		r.resolveChampionshipManager(),
		r.resolveRaceWeekendManager(),
		r.resolveServerProcess(),
		r.resolveScheduledRacesManager(),
	)

	return r.serverAdministrationHandler
}

func (r *Resolver) resolveContentUploadHandler() *ContentUploadHandler {
	if r.contentUploadHandler != nil {
		return r.contentUploadHandler
	}

	r.contentUploadHandler = NewContentUploadHandler(r.resolveBaseHandler(), r.resolveCarManager(), r.resolveTrackManager())

	return r.contentUploadHandler
}

func (r *Resolver) resolveRaceControlHub() *racecontrol.RaceControlHub {
	if r.raceControlHub != nil {
		return r.raceControlHub
	}

	r.raceControlHub = racecontrol.NewRaceControlHub()
	go panicCapture(r.raceControlHub.Run)

	return r.raceControlHub
}

func (r *Resolver) ResolveRaceControl() *racecontrol.RaceControl {
	if r.raceControl != nil {
		return r.raceControl
	}

	r.raceControl = racecontrol.NewRaceControl(
		r.config,
		r.resolveRaceControlHub(),
		racecontrol.NewFilesystemTrackData(r.resolveEnv()),
		r.resolveServerProcess(),
		r.ResolveStore(),
		r.resolvePenaltiesManager(),
		r.resolveEnv(),
	)

	return r.raceControl
}

func (r *Resolver) resolveRaceControlHandler() *racecontrol.RaceControlHandler {
	if r.config.Server.PerformanceMode {
		return nil
	}

	if r.raceControlHandler != nil {
		return r.raceControlHandler
	}

	r.raceControlHandler = racecontrol.NewRaceControlHandler(
		r.resolveBaseHandler(),
		r.ResolveStore(),
		r.resolveRaceManager(),
		r.ResolveRaceControl(),
		r.resolveRaceControlHub(),
		r.resolveServerProcess(),
		r.resolveEnv(),
	)

	return r.raceControlHandler
}

func (r *Resolver) resolveRaceWeekendManager() *smraceweekend.RaceWeekendManager {
	if r.raceWeekendManager != nil {
		return r.raceWeekendManager
	}

	r.raceWeekendManager = smraceweekend.NewRaceWeekendManager(
		r.config,
		r.resolveRaceManager(),
		r.resolveChampionshipManager(),
		r.ResolveStore(),
		r.resolveServerProcess(),
		r.resolveNotificationManager(),
		r.resolveCarManager(),
		r.resolveEnv(),
	)

	return r.raceWeekendManager
}

func (r *Resolver) resolveRaceWeekendHandler() *smraceweekend.RaceWeekendHandler {
	if r.raceWeekendHandler != nil {
		return r.raceWeekendHandler
	}

	r.raceWeekendHandler = smraceweekend.NewRaceWeekendHandler(r.resolveBaseHandler(), r.resolveRaceWeekendManager())

	return r.raceWeekendHandler
}

func (r *Resolver) resolveDiscordManager() *notification.DiscordManager {
	if r.discordManager != nil {
		return r.discordManager
	}

	// if manager errors, it will log the error and return discordManager flagged as disabled, so no need to handle err
	r.discordManager, _ = notification.NewDiscordManager(r.store, r.resolveScheduledRacesManager(), r.resolveEnv())

	return r.discordManager
}

func (r *Resolver) resolveNotificationManager() *notification.Manager {
	if r.notificationManager != nil {
		return r.notificationManager
	}

	r.notificationManager = notification.NewManager(r.resolveDiscordManager(), r.resolveCarManager(), r.store, r.resolveEnv())

	return r.notificationManager
}

func (r *Resolver) resolveStrackerHandler() *StrackerHandler {
	if r.strackerHandler != nil {
		return r.strackerHandler
	}

	r.strackerHandler = NewStrackerHandler(r.resolveBaseHandler(), r.ResolveStore())

	return r.strackerHandler
}

func (r *Resolver) resolveHealthCheck() *HealthCheck {
	if r.healthCheck != nil {
		return r.healthCheck
	}

	r.healthCheck = NewHealthCheck(r.ResolveRaceControl(), r.ResolveStore(), r.resolveServerProcess())

	return r.healthCheck
}

func (r *Resolver) resolveKissMyRankHandler() *KissMyRankHandler {
	if r.kissMyRankHandler != nil {
		return r.kissMyRankHandler
	}

	r.kissMyRankHandler = NewKissMyRankHandler(
		r.resolveBaseHandler(),
		r.ResolveStore(),
	)

	return r.kissMyRankHandler
}

func (r *Resolver) resolveRealPenaltyHandler() *RealPenaltyHandler {
	if r.realPenaltyHandler != nil {
		return r.realPenaltyHandler
	}

	r.realPenaltyHandler = NewRealPenaltyHandler(
		r.resolveBaseHandler(),
		r.ResolveStore(),
	)

	return r.realPenaltyHandler
}

func (r *Resolver) ResolveRouter(fs http.FileSystem) http.Handler {
	return Router(
		r.config,
		fs,
		r.resolveQuickRaceHandler(),
		r.resolveCustomRaceHandler(),
		r.resolveEventDetailsHandler(),
		r.resolveChampionshipsHandler(),
		r.resolveAccountHandler(),
		r.resolveAuditLogHandler(),
		r.resolveCarsHandler(),
		r.resolveTracksHandler(),
		r.resolveWeatherHandler(),
		r.resolvePenaltiesHandler(),
		r.resolveResultsHandler(),
		r.resolveContentUploadHandler(),
		r.resolveServerAdministrationHandler(),
		r.resolveRaceControlHandler(),
		r.resolveScheduledRacesHandler(),
		r.resolveRaceWeekendHandler(),
		r.resolveStrackerHandler(),
		r.resolveHealthCheck(),
		r.resolveKissMyRankHandler(),
		r.resolveRealPenaltyHandler(),
	)
}

// globalsEnv returns the environment of the managers, which reads the package variables of the root package. The
// values are read when they are needed, because the install path and the server id are set after the resolver has
// been created.
func globalsEnv() *serverenv.Env {
	return &serverenv.Env{
		ServerID:        func() smmodel.ServerID { return serverID },
		InstallPath:     func() string { return ServerInstallPath },
		FallBackSorting: func() bool { return UseFallBackSorting },
		PanicCapture:    func(fn func()) { panicCapture(fn) },
		Debug:           Debug,
	}
}
