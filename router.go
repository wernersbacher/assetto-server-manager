package servermanager

import (
	smaccounts "github.com/wernersbacher/assetto-server-manager/internal/accounts"
	smchampionship "github.com/wernersbacher/assetto-server-manager/internal/championship"
	smconfig "github.com/wernersbacher/assetto-server-manager/internal/config"
	smrace "github.com/wernersbacher/assetto-server-manager/internal/race"
	racecontrol "github.com/wernersbacher/assetto-server-manager/internal/racecontrol"
	smraceweekend "github.com/wernersbacher/assetto-server-manager/internal/raceweekend"
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
	quickRaceHandler *smrace.QuickRaceHandler,
	customRaceHandler *smrace.CustomRaceHandler,
	eventDetailsHandler *EventDetailsHandler,
	championshipsHandler *smchampionship.ChampionshipsHandler,
	accountHandler *smaccounts.AccountHandler,
	auditLogHandler *smaccounts.AuditLogHandler,
	carsHandler *CarsHandler,
	tracksHandler *TracksHandler,
	weatherHandler *WeatherHandler,
	penaltiesHandler *racecontrol.PenaltiesHandler,
	resultsHandler *ResultsHandler,
	contentUploadHandler *ContentUploadHandler,
	serverAdministrationHandler *ServerAdministrationHandler,
	raceControlHandler *racecontrol.RaceControlHandler,
	scheduledRacesHandler *schedule.Handler,
	raceWeekendHandler *smraceweekend.RaceWeekendHandler,
	strackerHandler *StrackerHandler,
	healthCheck *HealthCheck,
	kissMyRankHandler *KissMyRankHandler,
	realPenaltyHandler *RealPenaltyHandler,
) http.Handler {
	r := chi.NewRouter()

	compressor := middleware.Compress(5)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(compressor)
	r.Use(panicHandler)
	r.Use(accountHandler.RequestScopeMiddleware)

	r.HandleFunc("/login", accountHandler.Login)
	r.HandleFunc("/logout", accountHandler.Logout)
	r.HandleFunc("/robots.txt", serverAdministrationHandler.robots)
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
		r.Get("/", serverAdministrationHandler.home)
		r.Get("/changelog", serverAdministrationHandler.changelog)

		r.Mount("/stracker/", http.HandlerFunc(strackerHandler.proxy))

		// content
		r.Get("/cars", carsHandler.list)
		r.Get("/cars/search.json", carsHandler.searchJSON)
		r.Get("/car/{car_id}", carsHandler.view)
		r.Get("/tracks", tracksHandler.list)
		r.Get("/track/{track_id}", tracksHandler.view)
		r.Get("/weather", weatherHandler.list)

		r.Get("/events.ics", scheduledRacesHandler.AllScheduledRacesICalHandler)
		r.Get("/event-details", eventDetailsHandler.view)

		// results
		r.Get("/results", resultsHandler.list)
		r.Get("/results/{fileName}", resultsHandler.view)
		r.HandleFunc("/results/{fileName}/collisions", resultsHandler.renderCollisions)
		r.HandleFunc("/results/download/{fileName}", resultsHandler.file)
		r.Get("/api/results", resultsHandler.listAPI)
		r.Get("/api/results/{fileName}", resultsHandler.viewAPI)

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

		FileServer(r, "/content", http.Dir(filepath.Join(ServerInstallPath, "content")), true)
		FileServer(r, "/setups/download", http.Dir(filepath.Join(ServerInstallPath, "setups")), true)

		r.Get("/content/tracks/{track}/ui/preview.png", AssetCacheHeaders(http.HandlerFunc(tracksHandler.trackImage), true))
		r.Get("/content/tracks/{track}/ui/{layout}/preview.png", AssetCacheHeaders(http.HandlerFunc(tracksHandler.trackImage), true))

		r.Get("/content/tracks/{track}/ui/ui_track.json", AssetCacheHeaders(http.HandlerFunc(tracksHandler.trackInfo), true))
		r.Get("/content/tracks/{track}/ui/{layout}/ui_track.json", AssetCacheHeaders(http.HandlerFunc(tracksHandler.trackInfo), true))

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
		r.Post("/setups/upload", carSetupsUploadHandler)
		r.HandleFunc("/car/{name}/tags", carsHandler.tags)
		r.Post("/car/{name}/metadata", carsHandler.saveMetadata)
		r.Post("/car/{name}/skin", carsHandler.uploadSkin)
		r.Post("/track/{name}/metadata", tracksHandler.saveMetadata)
		r.Post("/results/upload", resultsHandler.uploadHandler)
		r.HandleFunc("/results/combine", resultsHandler.combineResults)

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
		r.Get("/process/{action}", serverAdministrationHandler.serverProcess)
		r.Get("/logs", serverAdministrationHandler.logs)
		r.Get("/api/logs", serverAdministrationHandler.logsAPI)
		r.Get("/api/log-download/{logFile}", serverAdministrationHandler.logsDownload)

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
		r.Post("/results/{fileName}/edit", resultsHandler.edit)

		// live timings
		r.Post("/live-timing/save-frames", raceControlHandler.SaveIFrames)

		// endpoints
		r.Post("/api/track/upload", contentUploadHandler.upload(ContentTypeTrack))
		r.Post("/api/car/upload", contentUploadHandler.upload(ContentTypeCar))
		r.Post("/api/weather/upload", contentUploadHandler.upload(ContentTypeWeather))
		r.Post("/api/image-upload", contentUploadHandler.imageUpload)

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
		r.Post("/results/{fileName}/delete", resultsHandler.delete)

		r.Post("/track/{name}/delete", tracksHandler.delete)
		r.Post("/car/{name}/delete", carsHandler.delete)
		r.Post("/car/{name}/skin/delete", carsHandler.deleteSkin)
		r.Post("/weather/delete/{key}", weatherHandler.delete)
		r.Post("/setups/delete/{car}/{track}/{setup}", carSetupDeleteHandler)

		r.Get("/autofill-entrants", serverAdministrationHandler.autoFillEntrantList)
		r.Post("/autofill-entrants/delete/{entrantID}", serverAdministrationHandler.autoFillEntrantDelete)

		// race weekend
		r.Post("/race-weekend/{raceWeekendID}/session/{sessionID}/delete", raceWeekendHandler.DeleteSession)
	})

	// admins
	r.Group(func(r chi.Router) {
		r.Use(accountHandler.AdminAccessMiddleware)
		if config.Server.AuditLogging {
			r.Use(auditLogHandler.Middleware)
		}

		r.HandleFunc("/server-options", serverAdministrationHandler.options)
		r.HandleFunc("/blacklist", serverAdministrationHandler.blacklist)
		r.HandleFunc("/motd", serverAdministrationHandler.motd)
		r.HandleFunc("/current-config", serverAdministrationHandler.currentConfig)
		r.HandleFunc("/audit-logs", auditLogHandler.ViewLogs)
		r.HandleFunc("/accounts/new", accountHandler.CreateOrEditAccount)
		r.HandleFunc("/accounts/edit/{id}", accountHandler.CreateOrEditAccount)
		r.Post("/accounts/delete/{id}", accountHandler.DeleteAccount)
		r.HandleFunc("/accounts/reset-password/{id}", accountHandler.ResetPassword)
		r.HandleFunc("/accounts/toggle-open", accountHandler.ToggleServerOpenStatus)
		r.HandleFunc("/accounts", accountHandler.ManageAccounts)
		r.HandleFunc("/search-index", carsHandler.rebuildSearchIndex)

		r.HandleFunc("/restart-session", raceControlHandler.RestartSession)
		r.HandleFunc("/next-session", raceControlHandler.NextSession)
		r.HandleFunc("/broadcast-chat", raceControlHandler.BroadcastChat)
		r.HandleFunc("/admin-command", raceControlHandler.AdminCommand)
		r.HandleFunc("/kick-user", raceControlHandler.KickUser)
		r.HandleFunc("/send-chat", raceControlHandler.SendChat)
		r.HandleFunc("/countdown", raceControlHandler.Countdown)

		r.HandleFunc("/stracker/options", strackerHandler.options)
		r.HandleFunc("/kissmyrank/options", kissMyRankHandler.options)
		r.HandleFunc("/realpenalty/options", realPenaltyHandler.options)
		r.HandleFunc("/realpenalty/logs", realPenaltyHandler.downloadLogs)
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
