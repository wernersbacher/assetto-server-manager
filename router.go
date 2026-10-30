package servermanager

import (
	smaccounts "github.com/wernersbacher/assetto-server-manager/internal/accounts"
	"github.com/wernersbacher/assetto-server-manager/internal/admin"
	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	"github.com/wernersbacher/assetto-server-manager/internal/contenthttp"
	"github.com/wernersbacher/assetto-server-manager/internal/pluginhttp"
	smrace "github.com/wernersbacher/assetto-server-manager/internal/race"
	racecontrol "github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	smraceweekend "github.com/wernersbacher/assetto-server-manager/internal/raceweekend"
	"github.com/wernersbacher/assetto-server-manager/internal/resultshttp"
	"github.com/wernersbacher/assetto-server-manager/internal/schedule"
	smserver "github.com/wernersbacher/assetto-server-manager/internal/server"

	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-http-utils/etag"
	"github.com/sirupsen/logrus"
)

var (
	logOutput     = smserver.NewLogBuffer(smserver.MaxLogSizeBytes)
	pluginsOutput = smserver.NewLogBuffer(smserver.MaxLogSizeBytes)

	logMultiWriter io.Writer

	Debug = os.Getenv("DEBUG") == "true"
)

func InitLogging() {
	if !Debug {
		logrus.SetLevel(logrus.InfoLevel)
	} else {
		logrus.SetLevel(logrus.DebugLevel)
	}

	logFile, err := os.OpenFile("server-manager.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)

	if err == nil {
		logMultiWriter = io.MultiWriter(os.Stdout, logOutput, logFile)
	} else {
		logrus.WithError(err).Errorf("Could not create server manager log file")
		logMultiWriter = io.MultiWriter(os.Stdout, logOutput)
	}

	logrus.SetOutput(logMultiWriter)
}

func Router(
	config *smconfig.Configuration,
	fs http.FileSystem,
	installPath string,
	serverSelection func(http.Handler) http.Handler,
	quickRaceHandler *smrace.QuickRaceHandler,
	customRaceHandler *smrace.CustomRaceHandler,
	eventDetailsHandler *admin.EventDetailsHandler,
	championshipsHandler *smchampionship.ChampionshipsHandler,
	accountHandler *smaccounts.AccountHandler,
	auditLogHandler *smaccounts.AuditLogHandler,
	carsHandler *contenthttp.CarsHandler,
	tracksHandler *contenthttp.TracksHandler,
	weatherHandler *contenthttp.WeatherHandler,
	setupsHandler *contenthttp.SetupsHandler,
	penaltiesHandler *racecontrol.PenaltiesHandler,
	resultsHandler *resultshttp.ResultsHandler,
	contentUploadHandler *contenthttp.ContentUploadHandler,
	serverAdministrationHandler *admin.ServerAdministrationHandler,
	raceControlHandler *racecontrol.RaceControlHandler,
	scheduledRacesHandler *schedule.Handler,
	raceWeekendHandler *smraceweekend.RaceWeekendHandler,
	strackerHandler *pluginhttp.StrackerHandler,
	healthCheck *admin.HealthCheck,
	kissMyRankHandler *pluginhttp.KissMyRankHandler,
	realPenaltyHandler *pluginhttp.RealPenaltyHandler,
) http.Handler {
	r := chi.NewRouter()

	compressor := middleware.Compress(5)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(compressor)
	r.Use(panicHandler)
	r.Use(accountHandler.RequestScopeMiddleware)
	r.Use(serverSelection)

	r.HandleFunc("/login", accountHandler.Login)
	r.HandleFunc("/logout", accountHandler.Logout)
	r.HandleFunc("/robots.txt", serverAdministrationHandler.Robots)
	r.Handle("/metrics", prometheusMonitoringHandler())
	r.Get("/healthcheck.json", healthCheck.ServeHTTP)

	if Debug {
		r.Mount("/debug/", middleware.Profiler())
	}

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		// if a user comes from an stracker page and hits a 404, the likelihood is that they found a link that does
		// not have an stracker prefix added to it. Catch it, and forward them back to an URL that has the prefix.
		u, err := url.Parse(r.Referer())

		if err == nil && strings.HasPrefix(u.Path, "/stracker/") {
			// try to redirect back to /stracker/<url request>
			r.URL.Path = "/stracker" + r.URL.Path

			http.Redirect(w, r, r.URL.String(), http.StatusTemporaryRedirect)
			return
		}

		http.NotFound(w, r)
	})

	// readers
	r.Group(func(r chi.Router) {
		r.Use(accountHandler.ReadAccessMiddleware)

		// pages
		r.Get("/", serverAdministrationHandler.Home)
		r.Get("/changelog", serverAdministrationHandler.Changelog)

		r.Mount("/stracker/", http.HandlerFunc(strackerHandler.Proxy))

		// content
		r.Get("/cars", carsHandler.List)
		r.Get("/cars/search.json", carsHandler.SearchJSON)
		r.Get("/car/{car_id}", carsHandler.View)
		r.Get("/tracks", tracksHandler.List)
		r.Get("/track/{track_id}", tracksHandler.View)
		r.Get("/weather", weatherHandler.List)

		r.Get("/events.ics", scheduledRacesHandler.AllScheduledRacesICalHandler)
		r.Get("/event-details", eventDetailsHandler.View)

		// results
		r.Get("/results", resultsHandler.List)
		r.Get("/results/{fileName}", resultsHandler.View)
		r.HandleFunc("/results/{fileName}/collisions", resultsHandler.RenderCollisions)
		r.HandleFunc("/results/download/{fileName}", resultsHandler.File)
		r.Get("/api/results", resultsHandler.ListAPI)
		r.Get("/api/results/{fileName}", resultsHandler.ViewAPI)

		r.Get("/custom", customRaceHandler.List)

		// championships
		r.Get("/championships", championshipsHandler.List)
		r.Get("/championship/{championshipID}", championshipsHandler.View)
		r.Get("/championship/{championshipID}/export", championshipsHandler.Export)
		r.Get("/api/championships", championshipsHandler.ListAPI)
		r.Get("/api/championship/{championshipID}", championshipsHandler.ViewAPI)
		r.HandleFunc("/championship/{championshipID}/export-results", championshipsHandler.ExportResults)
		r.Get("/championship/{championshipID}/ics", championshipsHandler.IcalFeed)
		r.Get("/championship/{championshipID}/sign-up", championshipsHandler.SignUpForm)
		r.Post("/championship/{championshipID}/sign-up", championshipsHandler.SignUpForm)
		r.Get("/championship/{championshipID}/sign-up/steam", championshipsHandler.RedirectToSteamLogin(func(r *http.Request) string {
			return fmt.Sprintf("/championship/%s/sign-up", chi.URLParam(r, "championshipID"))
		}))

		// race control
		r.Group(func(r chi.Router) {
			r.Use(func(next http.Handler) http.Handler {
				fn := func(w http.ResponseWriter, req *http.Request) {
					if config.Server.PerformanceMode {
						http.NotFound(w, req)
					} else {
						next.ServeHTTP(w, req)
					}
				}

				return http.HandlerFunc(fn)
			})

			r.Get("/live-timing", raceControlHandler.LiveTiming)
			r.Get("/api/race-control", raceControlHandler.Websocket)
			r.Get("/api/server-status", raceControlHandler.ServerStatusAPI)
			r.Get("/api/servers", serverAdministrationHandler.ServersAPI)
		})

		// calendar
		r.Get("/calendar", scheduledRacesHandler.Calendar)
		r.Get("/calendar.json", scheduledRacesHandler.CalendarJSON)
		r.Get("/api/upcoming-races", scheduledRacesHandler.UpcomingAPI)

		// account management
		r.HandleFunc("/accounts/new-password", accountHandler.NewPassword)
		r.HandleFunc("/accounts/update", accountHandler.Update)
		r.Get("/accounts/update/steam", championshipsHandler.RedirectToSteamLogin(func(r *http.Request) string {
			return "/accounts/update"
		}))
		r.HandleFunc("/accounts/dismiss-changelog", accountHandler.DismissChangelog)

		FileServer(r, "/content", http.Dir(filepath.Join(installPath, "content")), true)
		FileServer(r, "/setups/download", http.Dir(filepath.Join(installPath, "setups")), true)

		r.Get("/content/tracks/{track}/ui/preview.png", AssetCacheHeaders(http.HandlerFunc(tracksHandler.TrackImage), true))
		r.Get("/content/tracks/{track}/ui/{layout}/preview.png", AssetCacheHeaders(http.HandlerFunc(tracksHandler.TrackImage), true))

		r.Get("/content/tracks/{track}/ui/ui_track.json", AssetCacheHeaders(http.HandlerFunc(tracksHandler.TrackInfo), true))
		r.Get("/content/tracks/{track}/ui/{layout}/ui_track.json", AssetCacheHeaders(http.HandlerFunc(tracksHandler.TrackInfo), true))

		// race weekends
		r.Get("/race-weekends", raceWeekendHandler.List)
		r.Get("/race-weekend/{raceWeekendID}", raceWeekendHandler.View)
		r.Get("/race-weekend/{raceWeekendID}/filters", raceWeekendHandler.ManageFilters)
		r.Get("/race-weekend/{raceWeekendID}/entrylist", raceWeekendHandler.ManageEntryList)
		r.Post("/race-weekend/{raceWeekendID}/grid-preview", raceWeekendHandler.GridPreview)
		r.Get("/race-weekend/{raceWeekendID}/entrylist-preview", raceWeekendHandler.EntryListPreview)
		r.Get("/race-weekend/{raceWeekendID}/export", raceWeekendHandler.Export)
	})

	// writers
	r.Group(func(r chi.Router) {
		r.Use(accountHandler.WriteAccessMiddleware)
		if config.Server.AuditLogging {
			r.Use(auditLogHandler.Middleware)
		}

		// content
		r.Post("/setups/upload", setupsHandler.Upload)
		r.HandleFunc("/car/{name}/tags", carsHandler.Tags)
		r.Post("/car/{name}/metadata", carsHandler.SaveMetadata)
		r.Post("/car/{name}/skin", carsHandler.UploadSkin)
		r.Post("/track/{name}/metadata", tracksHandler.SaveMetadata)
		r.Post("/results/upload", resultsHandler.UploadHandler)
		r.HandleFunc("/results/combine", resultsHandler.CombineResults)

		// races
		r.Get("/quick", quickRaceHandler.Create)
		r.Post("/quick/submit", quickRaceHandler.Submit)
		r.Get("/custom/new", customRaceHandler.CreateOrEdit)
		r.Get("/custom/load/{uuid}", customRaceHandler.Start)
		r.Post("/custom/schedule/{uuid}", customRaceHandler.Schedule)
		r.Get("/custom/schedule/{uuid}/remove", customRaceHandler.RemoveSchedule)
		r.Get("/custom/edit/{uuid}", customRaceHandler.CreateOrEdit)
		r.Get("/custom/star/{uuid}", customRaceHandler.Star)
		r.Get("/custom/loop/{uuid}", customRaceHandler.Loop)
		r.Post("/custom/new/submit", customRaceHandler.Submit)

		// server management
		r.Get("/process/{action}", serverAdministrationHandler.ServerProcess)
		r.Get("/logs", serverAdministrationHandler.Logs)
		r.Get("/api/logs", serverAdministrationHandler.LogsAPI)
		r.Get("/api/log-download/{logFile}", serverAdministrationHandler.LogsDownload)

		// championships
		r.Get("/championships/new", championshipsHandler.CreateOrEdit)
		r.Post("/championships/new/submit", championshipsHandler.Submit)
		r.Get("/championship/{championshipID}/edit", championshipsHandler.CreateOrEdit)
		r.Get("/championship/{championshipID}/duplicate", championshipsHandler.Duplicate)
		r.Get("/championship/{championshipID}/event", championshipsHandler.EventConfiguration)
		r.Post("/championship/{championshipID}/event/submit", championshipsHandler.SubmitEventConfiguration)
		r.Get("/championship/{championshipID}/event/{eventID}/start", championshipsHandler.StartEvent)
		r.Post("/championship/{championshipID}/event/{eventID}/schedule", championshipsHandler.ScheduleEvent)
		r.Get("/championship/{championshipID}/event/{eventID}/schedule/remove", championshipsHandler.ScheduleEventRemove)
		r.Get("/championship/{championshipID}/event/{eventID}/edit", championshipsHandler.EventConfiguration)
		r.Get("/championship/{championshipID}/event/{eventID}/practice", championshipsHandler.StartPracticeEvent)
		r.Get("/championship/{championshipID}/event/{eventID}/cancel", championshipsHandler.CancelEvent)
		r.Get("/championship/{championshipID}/event/{eventID}/restart", championshipsHandler.RestartEvent)
		r.Get("/championship/{championshipID}/event/{eventID}/duplicate", championshipsHandler.DuplicateEvent)

		r.Post("/championship/{championshipID}/driver-penalty/{classID}/{driverGUID}", championshipsHandler.DriverPenalty)
		r.Post("/championship/{championshipID}/team-penalty/{classID}/{team}", championshipsHandler.TeamPenalty)
		r.Get("/championship/{championshipID}/entrants", championshipsHandler.SignedUpEntrants)
		r.Get("/championship/{championshipID}/entrants.csv", championshipsHandler.SignedUpEntrantsCSV)
		r.Get("/championship/{championshipID}/entrant/{entrantGUID}", championshipsHandler.ModifyEntrantStatus)
		r.Post("/championship/{championshipID}/reorder-events", championshipsHandler.ReorderEvents)

		r.Get("/championship/import", championshipsHandler.ImportChampionship)
		r.Post("/championship/import", championshipsHandler.ImportChampionship)
		r.Get("/championship/{championshipID}/event/{eventID}/import", championshipsHandler.EventImport)
		r.Post("/championship/{championshipID}/event/{eventID}/import", championshipsHandler.EventImport)

		r.Get("/championship/{championshipID}/custom/list", championshipsHandler.ListCustomRacesForImport)
		r.Get("/championship/{championshipID}/custom/{eventID}/import", championshipsHandler.CustomRaceImport)
		r.Get("/championship/{championshipID}/race-weekend/list", championshipsHandler.ListRaceWeekendsForImport)
		r.Get("/championship/{championshipID}/race-weekend/{weekendID}/import", championshipsHandler.RaceWeekendImport)

		// penalties
		r.Post("/penalties/{sessionFile}/{driverGUID}", penaltiesHandler.ManagePenalty)

		// results
		r.Post("/results/{fileName}/edit", resultsHandler.Edit)

		// live timings
		r.Post("/live-timing/save-frames", raceControlHandler.SaveIFrames)

		// endpoints
		r.Post("/api/track/upload", contentUploadHandler.Upload(contenthttp.ContentTypeTrack))
		r.Post("/api/car/upload", contentUploadHandler.Upload(contenthttp.ContentTypeCar))
		r.Post("/api/weather/upload", contentUploadHandler.Upload(contenthttp.ContentTypeWeather))
		r.Post("/api/image-upload", contentUploadHandler.ImageUpload)

		// race weekend
		r.Get("/race-weekends/new", raceWeekendHandler.CreateOrEdit)
		r.Post("/race-weekends/new/submit", raceWeekendHandler.Submit)
		r.Post("/race-weekend/{raceWeekendID}/delete", raceWeekendHandler.Delete)
		r.Get("/race-weekend/{raceWeekendID}/edit", raceWeekendHandler.CreateOrEdit)
		r.Get("/race-weekend/{raceWeekendID}/session", raceWeekendHandler.SessionConfiguration)
		r.Post("/race-weekend/{raceWeekendID}/session/submit", raceWeekendHandler.SubmitSessionConfiguration)
		r.Get("/race-weekend/{raceWeekendID}/session/{sessionID}/edit", raceWeekendHandler.SessionConfiguration)
		r.Get("/race-weekend/{raceWeekendID}/session/{sessionID}/start", raceWeekendHandler.StartSession)
		r.Get("/race-weekend/{raceWeekendID}/session/{sessionID}/practice", raceWeekendHandler.StartPracticeSession)
		r.Get("/race-weekend/{raceWeekendID}/session/{sessionID}/restart", raceWeekendHandler.RestartSession)
		r.Get("/race-weekend/{raceWeekendID}/session/{sessionID}/cancel", raceWeekendHandler.CancelSession)
		r.Get("/race-weekend/{raceWeekendID}/session/{sessionID}/import", raceWeekendHandler.ImportSessionResults)
		r.Post("/race-weekend/{raceWeekendID}/session/{sessionID}/import", raceWeekendHandler.ImportSessionResults)
		r.Post("/race-weekend/{raceWeekendID}/update-grid", raceWeekendHandler.UpdateGrid)
		r.Get("/race-weekend/{raceWeekendID}/update-entrylist", raceWeekendHandler.UpdateEntryList)
		r.Get("/race-weekend/import", raceWeekendHandler.ImportRaceWeekend)
		r.Post("/race-weekend/import", raceWeekendHandler.ImportRaceWeekend)
		r.Post("/race-weekend/{raceWeekendID}/session/{sessionID}/schedule", raceWeekendHandler.ScheduleSession)
		r.Get("/race-weekend/{raceWeekendID}/session/{sessionID}/schedule/remove", raceWeekendHandler.RemoveSessionSchedule)
	})

	// deleters
	r.Group(func(r chi.Router) {
		r.Use(accountHandler.DeleteAccessMiddleware)
		if config.Server.AuditLogging {
			r.Use(auditLogHandler.Middleware)
		}

		r.Post("/championship/{championshipID}/event/{eventID}/delete", championshipsHandler.DeleteEvent)
		r.Post("/championship/{championshipID}/delete", championshipsHandler.Delete)
		r.Post("/custom/delete/{uuid}", customRaceHandler.Delete)
		r.Post("/results/{fileName}/delete", resultsHandler.Delete)

		r.Post("/track/{name}/delete", tracksHandler.Delete)
		r.Post("/car/{name}/delete", carsHandler.Delete)
		r.Post("/car/{name}/skin/delete", carsHandler.DeleteSkin)
		r.Post("/weather/delete/{key}", weatherHandler.Delete)
		r.Post("/setups/delete/{car}/{track}/{setup}", setupsHandler.Delete)

		r.Get("/autofill-entrants", serverAdministrationHandler.AutoFillEntrantList)
		r.Post("/autofill-entrants/delete/{entrantID}", serverAdministrationHandler.AutoFillEntrantDelete)

		// race weekend
		r.Post("/race-weekend/{raceWeekendID}/session/{sessionID}/delete", raceWeekendHandler.DeleteSession)
	})

	// admins
	r.Group(func(r chi.Router) {
		r.Use(accountHandler.AdminAccessMiddleware)
		if config.Server.AuditLogging {
			r.Use(auditLogHandler.Middleware)
		}

		r.HandleFunc("/server-options", serverAdministrationHandler.Options)
		r.HandleFunc("/blacklist", serverAdministrationHandler.Blacklist)
		r.HandleFunc("/motd", serverAdministrationHandler.MOTD)
		r.HandleFunc("/current-config", serverAdministrationHandler.CurrentConfig)
		r.HandleFunc("/audit-logs", auditLogHandler.ViewLogs)
		r.HandleFunc("/accounts/new", accountHandler.CreateOrEditAccount)
		r.HandleFunc("/accounts/edit/{id}", accountHandler.CreateOrEditAccount)
		r.Post("/accounts/delete/{id}", accountHandler.DeleteAccount)
		r.HandleFunc("/accounts/reset-password/{id}", accountHandler.ResetPassword)
		r.HandleFunc("/accounts/toggle-open", accountHandler.ToggleServerOpenStatus)
		r.HandleFunc("/accounts", accountHandler.ManageAccounts)
		r.HandleFunc("/search-index", carsHandler.RebuildSearchIndex)

		r.HandleFunc("/restart-session", raceControlHandler.RestartSession)
		r.HandleFunc("/next-session", raceControlHandler.NextSession)
		r.HandleFunc("/broadcast-chat", raceControlHandler.BroadcastChat)
		r.HandleFunc("/admin-command", raceControlHandler.AdminCommand)
		r.HandleFunc("/kick-user", raceControlHandler.KickUser)
		r.HandleFunc("/send-chat", raceControlHandler.SendChat)
		r.HandleFunc("/countdown", raceControlHandler.Countdown)

		r.HandleFunc("/stracker/options", strackerHandler.Options)
		r.HandleFunc("/kissmyrank/options", kissMyRankHandler.Options)
		r.HandleFunc("/realpenalty/options", realPenaltyHandler.Options)
		r.HandleFunc("/realpenalty/logs", realPenaltyHandler.DownloadLogs)
	})

	FileServer(r, "/static", fs, false)

	return prometheusMonitoringWrapper(r)
}

func FileServer(r chi.Router, path string, root http.FileSystem, useRevalidation bool) {
	if strings.ContainsAny(path, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	fs := http.StripPrefix(path, http.FileServer(root))

	if path != "/" && path[len(path)-1] != '/' {
		r.Get(path, http.RedirectHandler(path+"/", http.StatusMovedPermanently).ServeHTTP)
		path += "/"
	}
	path += "*"

	r.Get(path, AssetCacheHeaders(fs, useRevalidation))
}

const maxAge30Days = 2592000

func AssetCacheHeaders(next http.Handler, useRevalidation bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if useRevalidation {
			w.Header().Add("Cache-Control", "public, must-revalidate")
			etag.Handler(next, false).ServeHTTP(w, r)
		} else {
			w.Header().Add("Cache-Control", fmt.Sprintf("public, max-age=%d", maxAge30Days))

			next.ServeHTTP(w, r)
		}
	}
}
