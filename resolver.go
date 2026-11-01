package servermanager

import (
	smaccounts "github.com/wernersbacher/ACPitwall/internal/accounts"
	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/admin"
	"github.com/wernersbacher/ACPitwall/internal/carclasses"
	smchampionship "github.com/wernersbacher/ACPitwall/internal/championship"
	smconfig "github.com/wernersbacher/ACPitwall/internal/config"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/contenthttp"
	"github.com/wernersbacher/ACPitwall/internal/downloads"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	"github.com/wernersbacher/ACPitwall/internal/notification"
	"github.com/wernersbacher/ACPitwall/internal/pluginhttp"
	smrace "github.com/wernersbacher/ACPitwall/internal/race"
	racecontrol "github.com/wernersbacher/ACPitwall/internal/racecontrol"
	smraceweekend "github.com/wernersbacher/ACPitwall/internal/raceweekend"
	"github.com/wernersbacher/ACPitwall/internal/resultshttp"
	"github.com/wernersbacher/ACPitwall/internal/schedule"
	smserver "github.com/wernersbacher/ACPitwall/internal/server"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	"github.com/wernersbacher/ACPitwall/internal/servers"
	"github.com/wernersbacher/ACPitwall/internal/skins"
	"github.com/wernersbacher/ACPitwall/internal/stats"
	"github.com/wernersbacher/ACPitwall/internal/statshttp"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	"github.com/wernersbacher/ACPitwall/internal/teams"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"

	"net/http"

	"github.com/wernersbacher/ACPitwall/cmd/acpitwall/static"

	"github.com/cj123/sessions"
	"github.com/wernersbacher/ACPitwall/pkg/udp"
)

type Resolver struct {
	config          *smconfig.Configuration
	store           smstore.Store
	state           *serverenv.State
	sessionStore    sessions.Store
	templateLoader  smweb.TemplateLoader
	reloadTemplates bool

	// registry holds the runtime stack of every server slot, slotConfigs are the configured slots in the same order.
	// The stacks are built by resolveSlot. The single-slot accessors (resolveServerProcess, resolveRaceManager, ...)
	// return the components of the default slot until the handlers are routed to the slot of the request.
	registry     *servers.Registry
	slotConfigs  []smconfig.Slot
	slotResolved map[*servers.Slot]bool

	carManager            *smcontent.CarManager
	trackManager          *smcontent.TrackManager
	accountManager        *smaccounts.AccountManager
	discordManager        *notification.DiscordManager
	notificationManager   *notification.Manager
	scheduledRacesManager *schedule.Manager

	viewRenderer *smweb.Renderer

	// handlers
	baseHandler                 *smweb.BaseHandler
	quickRaceHandler            *smrace.QuickRaceHandler
	eventDetailsHandler         *admin.EventDetailsHandler
	customRaceHandler           *smrace.CustomRaceHandler
	championshipsHandler        *smchampionship.ChampionshipsHandler
	accountHandler              *smaccounts.AccountHandler
	auditLogHandler             *smaccounts.AuditLogHandler
	carsHandler                 *contenthttp.CarsHandler
	downloadSigner              *downloads.Signer
	tracksHandler               *contenthttp.TracksHandler
	statsIndex                  *stats.Index
	contentRecords              *contenthttp.Records
	weatherHandler              *contenthttp.WeatherHandler
	setupsHandler               *contenthttp.SetupsHandler
	penaltiesHandler            *racecontrol.PenaltiesHandler
	penaltiesManager            *racecontrol.PenaltiesManager
	resultsHandler              *resultshttp.ResultsHandler
	driverHandler               *statshttp.DriverHandler
	leaderboardHandler          *statshttp.LeaderboardHandler
	scheduledRacesHandler       *schedule.Handler
	contentUploadHandler        *contenthttp.ContentUploadHandler
	raceControlHandler          *racecontrol.RaceControlHandler
	serverAdministrationHandler *admin.ServerAdministrationHandler
	raceWeekendHandler          *smraceweekend.RaceWeekendHandler
	strackerHandler             *pluginhttp.StrackerHandler
	healthCheck                 *admin.HealthCheck
	kissMyRankHandler           *pluginhttp.KissMyRankHandler
	realPenaltyHandler          *pluginhttp.RealPenaltyHandler
	teamManager                 *teams.Manager
	skinManager                 *skins.Manager
	carClassManager             *carclasses.Manager
	carClassHandler             *carclasses.Handler
	teamsHandler                *teams.Handler
}

func NewResolver(config *smconfig.Configuration, templateLoader smweb.TemplateLoader, reloadTemplates bool, store smstore.Store, state *serverenv.State) (*Resolver, error) {
	r := &Resolver{
		state:           state,
		config:          config,
		templateLoader:  templateLoader,
		reloadTemplates: reloadTemplates,
		store:           store,
		slotResolved:    make(map[*servers.Slot]bool),
	}

	slotConfigs, err := config.Slots()

	if err != nil {
		return nil, err
	}

	registry, err := servers.NewRegistry(slotConfigs, state, func(fn func()) { panicCapture(fn) }, Debug)

	if err != nil {
		return nil, err
	}

	r.slotConfigs = slotConfigs
	r.registry = registry

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

func (r *Resolver) initViewRenderer() error {
	if r.viewRenderer != nil {
		return nil
	}

	viewRenderer, err := smweb.NewRenderer(smweb.RendererOptions{
		Config:       r.config,
		Store:        r.store,
		Loader:       r.templateLoader,
		ServerFor:    r.serverContextFor,
		Servers:      r.serverRefs,
		BuildVersion: BuildVersion,
		Debug:        Debug,
		Reload:       r.reloadTemplates,
		Funcs:        serverTemplateFuncs(r.resolveEnv(), r.resultsServers),
	})

	if err != nil {
		return err
	}

	r.viewRenderer = viewRenderer

	return nil
}

// resolveEnv returns the environment of the managers which are shared by all slots: that of the default slot.
func (r *Resolver) resolveEnv() *serverenv.Env {
	return r.registry.Default().Env
}

func (r *Resolver) ResolveStore() smstore.Store {
	return r.store
}

// Servers returns the registry of the server slots, with the runtime stack of every slot built.
func (r *Resolver) Servers() *servers.Registry {
	for _, slot := range r.registry.All() {
		r.resolveSlot(slot)
	}

	return r.registry
}

// slotConfig returns the configuration of a slot of the registry.
func (r *Resolver) slotConfig(slot *servers.Slot) smconfig.Slot {
	for i, s := range r.registry.All() {
		if s == slot {
			return r.slotConfigs[i]
		}
	}

	panic("servermanager: slot is not part of the registry")
}

// resolveSlot builds the runtime stack of a slot, once: the server process, the content manager wrapper, race control
// with its hub and the race, championship and race weekend managers. The stack works on the data of the slot (see
// smstore.ForServer) and the Env of the slot, and UDP messages of its process only reach the managers of the slot.
// Cars, tracks, notifications and penalties are shared by all slots.
func (r *Resolver) resolveSlot(slot *servers.Slot) *servers.Slot {
	if r.slotResolved[slot] {
		return slot
	}

	r.slotResolved[slot] = true

	slotConfig := r.slotConfig(slot)
	store := smstore.ForServer(r.store, smmodel.ServerID(slotConfig.ID))
	slot.Store = store

	// the process reads the executable and the plugin commands of the slot from the configuration
	processConfig := *r.config
	processConfig.Steam.ExecutablePath = slot.ExecutablePath
	processConfig.Server.Plugins = slotConfig.Plugins

	slot.ContentManagerWrapper = smserver.NewContentManagerWrapper(r.config, store, r.resolveCarManager(), r.resolveTrackManager(), slot.Env)
	slot.ContentManagerWrapper.SetSkinDownloads(r.resolveDownloadSigner(), func() downloads.Access { return r.resolveAccountHandler().DownloadAccess() })
	slot.ContentManagerWrapper.SetUserSkinDirs(r.userSkinDir)
	process := smserver.NewAssettoServerProcess(&processConfig, r.slotUDPCallback(slot), store, slot.ContentManagerWrapper, slot.Env, pluginsOutput)
	process.SetStartCheck(func() error { return r.checkPorts(slot) })
	slot.Process = process

	slot.RaceControlHub = racecontrol.NewRaceControlHub()
	go panicCapture(slot.RaceControlHub.Run)

	slot.RaceControl = racecontrol.NewRaceControl(
		r.config,
		slot.RaceControlHub,
		racecontrol.NewFilesystemTrackData(slot.Env),
		slot.Process,
		store,
		r.resolvePenaltiesManager(),
		slot.Env,
	)

	slot.RaceManager = smrace.NewRaceManager(
		r.config,
		store,
		slot.Process,
		r.resolveCarManager(),
		r.resolveTrackManager(),
		r.resolveNotificationManager(),
		slot.RaceControl,
		slot.Env,
	)

	slot.ChampionshipManager = smchampionship.NewChampionshipManager(
		slot.RaceManager,
		r.config,
		store,
		slot.Process,
		slot.Env,
		r.resolveNotificationManager(),
	)
	slot.ChampionshipManager.SetTeamDirectory(r.resolveTeamManager())
	slot.ChampionshipManager.SetSkinDirectory(r.resolveSkinManager())

	slot.RaceWeekendManager = smraceweekend.NewRaceWeekendManager(
		r.config,
		slot.RaceManager,
		slot.ChampionshipManager,
		store,
		slot.Process,
		r.resolveNotificationManager(),
		r.resolveCarManager(),
		slot.Env,
	)

	return slot
}

// slotUDPCallback returns the callback of the process of a slot. It dispatches to the managers of that slot only.
func (r *Resolver) slotUDPCallback(slot *servers.Slot) func(message udp.Message) {
	return func(message udp.Message) {
		slot.DispatchUDP(message, r.config.Server.PerformanceMode)
	}
}

// resolveDefaultSlot returns the default slot with its stack built.
func (r *Resolver) resolveDefaultSlot() *servers.Slot {
	return r.resolveSlot(r.registry.Default())
}

func (r *Resolver) resolveServerProcess() smserver.ServerProcess {
	return r.resolveDefaultSlot().Process
}

func (r *Resolver) resolveContentManagerWrapper() *smserver.ContentManagerWrapper {
	return r.resolveDefaultSlot().ContentManagerWrapper
}

func (r *Resolver) resolveRaceManager() *smrace.RaceManager {
	return r.resolveDefaultSlot().RaceManager
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
		r.raceManagerFor,
		r.ResolveStore(),
	)

	return r.customRaceHandler
}

func (r *Resolver) resolveEventDetailsHandler() *admin.EventDetailsHandler {
	if r.eventDetailsHandler != nil {
		return r.eventDetailsHandler
	}

	r.eventDetailsHandler = admin.NewEventDetailsHandler(r.resolveBaseHandler(), r.adminSlotFor)

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

	r.quickRaceHandler = smrace.NewQuickRaceHandler(r.config, r.resolveBaseHandler(), r.raceManagerFor)

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
		r.state.InstallPath(),
		r.config.Server.ScanContentFolderForChanges,
		r.config.Server.UseCarNameCache,
	)

	r.carManager.PanicCapture = func(fn func()) { panicCapture(fn) }

	return r.carManager
}

func (r *Resolver) resolveCarsHandler() *contenthttp.CarsHandler {
	if r.carsHandler != nil {
		return r.carsHandler
	}

	r.carsHandler = contenthttp.NewCarsHandler(r.resolveBaseHandler(), r.resolveCarManager(), r.resolveTrackManager(), r.resolveEnv(), r.resolveRecords(), r.resolveDownloadSigner())
	r.carsHandler.SetUserSkins(r.resolveSkinManager())
	r.carsHandler.SetSkinInfo(skinInfo{r})

	return r.carsHandler
}

func (r *Resolver) resolveChampionshipManager() *smchampionship.ChampionshipManager {
	return r.resolveDefaultSlot().ChampionshipManager
}

func (r *Resolver) resolveChampionshipsHandler() *smchampionship.ChampionshipsHandler {
	if r.championshipsHandler != nil {
		return r.championshipsHandler
	}

	r.championshipsHandler = smchampionship.NewChampionshipsHandler(r.config, r.resolveBaseHandler(), r.championshipManagerFor, r.registry)

	return r.championshipsHandler
}

func (r *Resolver) resolveTrackManager() *smcontent.TrackManager {
	if r.trackManager != nil {
		return r.trackManager
	}

	r.trackManager = smcontent.NewTrackManager(r.state.InstallPath())

	return r.trackManager
}

// resolveStatsIndex returns the results statistics index, shared by all handlers. The loader of the default slot reads the
// results of all slots.
func (r *Resolver) resolveStatsIndex() *stats.Index {
	if r.statsIndex != nil {
		return r.statsIndex
	}

	r.statsIndex = stats.New(r.resolveEnv().Results(), stats.DefaultRefreshInterval)

	return r.statsIndex
}

// resolveRecords returns the lap records source of the track and car pages and the records API.
func (r *Resolver) resolveRecords() *contenthttp.Records {
	if r.contentRecords != nil {
		return r.contentRecords
	}

	r.contentRecords = contenthttp.NewRecords(r.resolveStatsIndex(), r.resultsServers)
	r.contentRecords.Hide = resultshttp.HideFuncFor(r.ResolveStore())
	r.contentRecords.CarClasses = r.resolveCarClassManager()

	return r.contentRecords
}

func (r *Resolver) resolveTracksHandler() *contenthttp.TracksHandler {
	if r.tracksHandler != nil {
		return r.tracksHandler
	}

	r.tracksHandler = contenthttp.NewTracksHandler(r.resolveBaseHandler(), r.resolveTrackManager(), r.resolveEnv(), r.resolveRecords(), func() []byte {
		return static.FSMustByte(false, "/img/no-preview-general.png")
	})

	return r.tracksHandler
}

func (r *Resolver) resolveWeatherHandler() *contenthttp.WeatherHandler {
	if r.weatherHandler != nil {
		return r.weatherHandler
	}

	r.weatherHandler = contenthttp.NewWeatherHandler(r.resolveBaseHandler(), r.resolveEnv())

	return r.weatherHandler
}

func (r *Resolver) resolveSetupsHandler() *contenthttp.SetupsHandler {
	if r.setupsHandler != nil {
		return r.setupsHandler
	}

	r.setupsHandler = contenthttp.NewSetupsHandler(r.resolveEnv())

	return r.setupsHandler
}

func (r *Resolver) resolvePenaltiesHandler() *racecontrol.PenaltiesHandler {
	if r.penaltiesHandler != nil {
		return r.penaltiesHandler
	}

	r.penaltiesHandler = racecontrol.NewPenaltiesHandler(r.resolveBaseHandler(), r.resolvePenaltiesManager())

	return r.penaltiesHandler
}

func (r *Resolver) resolvePenaltiesManager() *racecontrol.PenaltiesManager {
	if r.penaltiesManager != nil {
		return r.penaltiesManager
	}

	r.penaltiesManager = racecontrol.NewPenaltiesManager(r.ResolveStore(), r.resolveEnv())

	return r.penaltiesManager
}

func (r *Resolver) resolveResultsHandler() *resultshttp.ResultsHandler {
	if r.resultsHandler != nil {
		return r.resultsHandler
	}

	r.resultsHandler = resultshttp.NewResultsHandler(r.resolveBaseHandler(), r.ResolveStore(), r.resolveEnv(), r.resultsServers)
	r.resultsHandler.CarClasses = r.resolveCarClassManager()

	return r.resultsHandler
}

// resolveDriverHandler returns the handler of the driver profile page and API.
func (r *Resolver) resolveDriverHandler() *statshttp.DriverHandler {
	if r.driverHandler != nil {
		return r.driverHandler
	}

	r.driverHandler = statshttp.NewDriverHandler(r.resolveBaseHandler(), r.resolveStatsIndex(), r.resultsServers, r.championshipManagerFor)
	r.driverHandler.Hide = resultshttp.HideFuncFor(r.ResolveStore())

	return r.driverHandler
}

func (r *Resolver) resolveScheduledRacesManager() *schedule.Manager {
	if r.scheduledRacesManager != nil {
		return r.scheduledRacesManager
	}

	r.scheduledRacesManager = schedule.NewManager(r.config, r.ResolveStore(), r.resolveEnv(), func(session *smmodel.RaceWeekendSession) (serverconfig.EntryList, error) {
		// resolved when called, because the race weekend manager needs the notification manager, which needs this manager
		return r.resolveRaceWeekendManager().SessionEntryList(session, session.RaceWeekend())
	}, r.serverRefs)

	return r.scheduledRacesManager
}

func (r *Resolver) resolveScheduledRacesHandler() *schedule.Handler {
	if r.scheduledRacesHandler != nil {
		return r.scheduledRacesHandler
	}

	r.scheduledRacesHandler = schedule.NewHandler(r.resolveBaseHandler(), r.resolveScheduledRacesManager())

	return r.scheduledRacesHandler
}

func (r *Resolver) resolveServerAdministrationHandler() *admin.ServerAdministrationHandler {
	if r.serverAdministrationHandler != nil {
		return r.serverAdministrationHandler
	}

	r.serverAdministrationHandler = admin.NewServerAdministrationHandler(
		r.config,
		r.resolveBaseHandler(),
		r.adminSlotFor,
		r.adminSlots,
		r.portWarnings,
		r.resolveScheduledRacesManager(),
		admin.Logs{Manager: logOutput, Plugins: pluginsOutput},
		changelogHTML,
		r.state.SetFallBackSorting,
	)

	return r.serverAdministrationHandler
}

func (r *Resolver) resolveContentUploadHandler() *contenthttp.ContentUploadHandler {
	if r.contentUploadHandler != nil {
		return r.contentUploadHandler
	}

	r.contentUploadHandler = contenthttp.NewContentUploadHandler(r.resolveBaseHandler(), r.resolveCarManager(), r.resolveTrackManager(), r.resolveEnv())

	return r.contentUploadHandler
}

func (r *Resolver) resolveRaceControlHub() *racecontrol.RaceControlHub {
	return r.resolveDefaultSlot().RaceControlHub
}

func (r *Resolver) ResolveRaceControl() *racecontrol.RaceControl {
	return r.resolveDefaultSlot().RaceControl
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
		r.raceControlSlotFor,
	)

	return r.raceControlHandler
}

func (r *Resolver) resolveRaceWeekendManager() *smraceweekend.RaceWeekendManager {
	return r.resolveDefaultSlot().RaceWeekendManager
}

func (r *Resolver) resolveRaceWeekendHandler() *smraceweekend.RaceWeekendHandler {
	if r.raceWeekendHandler != nil {
		return r.raceWeekendHandler
	}

	r.raceWeekendHandler = smraceweekend.NewRaceWeekendHandler(r.resolveBaseHandler(), r.raceWeekendManagerFor, r.registry)

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

func (r *Resolver) resolveStrackerHandler() *pluginhttp.StrackerHandler {
	if r.strackerHandler != nil {
		return r.strackerHandler
	}

	r.strackerHandler = pluginhttp.NewStrackerHandler(r.resolveBaseHandler(), r.pluginSlotFor, r.portWarnings)

	return r.strackerHandler
}

func (r *Resolver) resolveHealthCheck() *admin.HealthCheck {
	if r.healthCheck != nil {
		return r.healthCheck
	}

	r.healthCheck = admin.NewHealthCheck(r.adminSlotFor, BuildVersion)

	return r.healthCheck
}

func (r *Resolver) resolveKissMyRankHandler() *pluginhttp.KissMyRankHandler {
	if r.kissMyRankHandler != nil {
		return r.kissMyRankHandler
	}

	r.kissMyRankHandler = pluginhttp.NewKissMyRankHandler(r.resolveBaseHandler(), r.pluginSlotFor, r.portWarnings)

	return r.kissMyRankHandler
}

func (r *Resolver) resolveRealPenaltyHandler() *pluginhttp.RealPenaltyHandler {
	if r.realPenaltyHandler != nil {
		return r.realPenaltyHandler
	}

	r.realPenaltyHandler = pluginhttp.NewRealPenaltyHandler(r.resolveBaseHandler(), r.pluginSlotFor, r.portWarnings)

	return r.realPenaltyHandler
}

func (r *Resolver) ResolveRouter(fs http.FileSystem) http.Handler {
	// the stacks of all slots have to exist before requests are served
	r.Servers()

	return Router(
		r.config,
		fs,
		r.state.InstallPath(),
		smweb.ServerSelectionMiddleware(r.isServerID),
		r.resolveQuickRaceHandler(),
		r.resolveCustomRaceHandler(),
		r.resolveEventDetailsHandler(),
		r.resolveChampionshipsHandler(),
		r.resolveAccountHandler(),
		r.resolveAuditLogHandler(),
		r.resolveCarsHandler(),
		r.resolveTracksHandler(),
		r.resolveWeatherHandler(),
		r.resolveSetupsHandler(),
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
		r.resolveDriverHandler(),
		r.resolveLeaderboardHandler(),
		r.resolveTeamsHandler(),
		r.resolveCarClassHandler(),
	)
}

// resolveTeamManager returns the manager of the teams, which are shared by all slots. It reads the team options of the
// account options on every use, follows the renaming of a team in the registrations of running championships and
// takes deleted accounts out of their teams.
func (r *Resolver) resolveTeamManager() *teams.Manager {
	if r.teamManager != nil {
		return r.teamManager
	}

	r.teamManager = teams.NewManager(r.store, func() teams.Options {
		disableCreation, maxOwned := r.resolveAccountHandler().TeamOptions()

		return teams.Options{DisableCreation: disableCreation, MaxOwnedTeams: maxOwned}
	})

	// the championships are shared by all slots, so the manager of the default slot can update all of them
	r.teamManager.SetRenameHook(func(team *smmodel.Team) error {
		return r.resolveChampionshipManager().RenameTeam(team)
	})

	r.resolveAccountHandler().SetAccountDeletedHook(r.teamManager.RemoveAccount)

	return r.teamManager
}

func (r *Resolver) resolveTeamsHandler() *teams.Handler {
	if r.teamsHandler != nil {
		return r.teamsHandler
	}

	r.teamsHandler = teams.NewHandler(r.resolveBaseHandler(), r.resolveTeamManager(), r.store, r.config.HTTP.BaseURL)

	return r.teamsHandler
}

// resolveLeaderboardHandler returns the handler of the hotlap leaderboard page and API.
func (r *Resolver) resolveLeaderboardHandler() *statshttp.LeaderboardHandler {
	if r.leaderboardHandler != nil {
		return r.leaderboardHandler
	}

	r.leaderboardHandler = statshttp.NewLeaderboardHandler(r.resolveBaseHandler(), r.resolveStatsIndex(), r.resultsServers, r.resolveEnv().InstallPath)
	r.leaderboardHandler.Hide = resultshttp.HideFuncFor(r.ResolveStore())
	r.leaderboardHandler.CarClasses = r.resolveCarClassManager()

	return r.leaderboardHandler
}

// resolveDownloadSigner returns the signer of the signed skin download URLs (one secret for all slots).
func (r *Resolver) resolveDownloadSigner() *downloads.Signer {
	if r.downloadSigner == nil {
		r.downloadSigner = downloads.NewSigner(r.store)
	}

	return r.downloadSigner
}
