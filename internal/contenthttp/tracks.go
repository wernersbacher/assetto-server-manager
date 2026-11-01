package contenthttp

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	"github.com/wernersbacher/ACPitwall/internal/plainname"
	smresults "github.com/wernersbacher/ACPitwall/internal/results"
	"github.com/wernersbacher/ACPitwall/internal/serverenv"
	"github.com/wernersbacher/ACPitwall/internal/stats"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// TracksHandler serves the track list, the track details, the track metadata and the track previews.
type TracksHandler struct {
	*smweb.BaseHandler

	trackManager *smcontent.TrackManager
	env          *serverenv.Env
	records      *Records

	// noPreview returns the image which is served for tracks without a preview.
	noPreview func() []byte
}

func NewTracksHandler(baseHandler *smweb.BaseHandler, trackManager *smcontent.TrackManager, env *serverenv.Env, records *Records, noPreview func() []byte) *TracksHandler {
	return &TracksHandler{
		BaseHandler:  baseHandler,
		trackManager: trackManager,
		env:          env,
		records:      records,
		noPreview:    noPreview,
	}
}

type trackListTemplateVars struct {
	smweb.BaseTemplateVars

	Tracks []smcontent.Track
}

func (th *TracksHandler) List(w http.ResponseWriter, r *http.Request) {
	tracks, err := th.trackManager.ListTracks()

	if err != nil {
		logrus.WithError(err).Errorf("could not get track list")
	}

	th.ViewRenderer.MustLoadTemplate(w, r, "content/tracks.html", &trackListTemplateVars{
		Tracks: tracks,
	})
}

func (th *TracksHandler) Delete(w http.ResponseWriter, r *http.Request) {
	trackName := chi.URLParam(r, "name")
	tracksPath := filepath.Join(th.env.InstallPath(), "content", "tracks")

	existingTracks, err := th.trackManager.ListTracks()

	if err != nil {
		logrus.WithError(err).Errorf("could not get track list")
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.content.track_list_failed"))
		http.Redirect(w, r, r.Referer(), http.StatusFound)
		return
	}

	var found bool

	for _, track := range existingTracks {
		if track.Name == trackName {
			// Delete track
			found = true

			err := os.RemoveAll(filepath.Join(tracksPath, trackName))

			if err != nil {
				found = false
				logrus.WithError(err).Errorf("could not remove track files")
			}

			for _, layout := range track.Layouts {
				smcontent.ClearTrackInfoCache(track.Name, layout)
			}

			break
		}
	}

	if found {
		// confirm deletion
		smweb.AddFlash(w, r, smweb.T(r, "flash.content.track_deleted"))
	} else {
		// inform track wasn't found
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.content.track_not_deleted"))
	}

	http.Redirect(w, r, "/tracks", http.StatusFound)
}

func (th *TracksHandler) View(w http.ResponseWriter, r *http.Request) {
	trackName := chi.URLParam(r, "track_id")

	if !plainname.Valid(trackName) {
		http.NotFound(w, r)
		return
	}

	recordsFilter, filterServer, classIDs, err := th.records.trackFilter(r)

	if smweb.FilterError(w, err) {
		return
	}

	templateParams, err := th.loadTrackDetailsForTemplate(trackName, r, recordsFilter, filterServer, classIDs)

	if os.IsNotExist(err) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Errorf("Could not load track details for: %s", trackName)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	th.ViewRenderer.MustLoadTemplate(w, r, "content/track-details.html", templateParams)
}

func (th *TracksHandler) SaveMetadata(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	if !plainname.Valid(name) {
		http.NotFound(w, r)
		return
	}

	if err := th.trackManager.UpdateTrackMetadata(name, r); err != nil {
		logrus.WithError(err).Errorf("Could not update track metadata for %s", name)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	smweb.AddFlash(w, r, smweb.T(r, "flash.content.track_metadata_updated"))
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

func (th *TracksHandler) TrackImage(w http.ResponseWriter, r *http.Request) {
	track := chi.URLParam(r, "track")
	layout := chi.URLParam(r, "layout")

	if !plainname.Valid(track) || (layout != "" && !plainname.Valid(layout)) {
		http.NotFound(w, r)
		return
	}

	w.Header().Add("Content-Type", "image/png")
	n, err := th.trackManager.GetTrackImage(w, track, layout)

	if err != nil {
		image := th.noPreview()
		_, _ = w.Write(image)
	} else {
		w.Header().Add("Content-Length", strconv.Itoa(int(n)))
	}
}

func (th *TracksHandler) TrackInfo(w http.ResponseWriter, r *http.Request) {
	track := chi.URLParam(r, "track")
	layout := chi.URLParam(r, "layout")

	if !plainname.Valid(track) || (layout != "" && !plainname.Valid(layout)) {
		http.NotFound(w, r)
		return
	}

	w.Header().Add("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(th.env.TrackInfo(track, layout))
}

type trackDetailsTemplateVars struct {
	smweb.BaseTemplateVars

	Track     *smcontent.Track
	TrackInfo map[string]*smcontent.TrackInfo
	Results   map[string][]smresults.SessionResults

	// Records are the best laps per car of every layout, fastest first (the first one is the layout record).
	Records map[string][]RecordRow
	// FilterServer is the id of the server the records are restricted to, empty for all.
	FilterServer string

	// CarClasses are the classes to restrict the records to (empty: there are none), CarClassIDs the chosen ones.
	CarClasses  []smweb.CarClassOption
	CarClassIDs []string
}

func (th *TracksHandler) loadTrackDetailsForTemplate(trackName string, r *http.Request, recordsFilter stats.Filter, filterServer string, classIDs []string) (*trackDetailsTemplateVars, error) {
	trackInfoMap := make(map[string]*smcontent.TrackInfo)
	resultsMap := make(map[string][]smresults.SessionResults)
	recordsMap := make(map[string][]RecordRow)

	track, err := th.trackManager.GetTrackFromName(trackName)

	if err != nil {
		return nil, err
	}

	err = track.LoadMetaData(th.env.InstallPath())

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load meta data for track: %s", trackName)
	}

	for _, layout := range track.Layouts {
		trackInfo, err := smcontent.GetTrackInfo(th.env.InstallPath(), track.Name, layout)

		if err != nil {
			logrus.WithError(err).Errorf("Couldn't load track info for layout: %s, track: %s", layout, track.Name)
			continue
		}

		trackInfoMap[layout] = trackInfo

		results, err := th.resultsForLayout(track.Name, layout, th.records.hideFunc(r))

		if err != nil {
			logrus.WithError(err).Errorf("Couldn't load results for layout: %s, track: %s", layout, track.Name)
			continue
		}

		resultsMap[layout] = results
		recordsMap[layout] = th.records.rows(th.records.forTrack(track.Name, layout, recordsFilter))
	}

	return &trackDetailsTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{},
		Track:            track,
		TrackInfo:        trackInfoMap,
		Results:          resultsMap,
		Records:          recordsMap,
		FilterServer:     filterServer,
		CarClasses:       th.records.carClassOptions(classIDs),
		CarClassIDs:      classIDs,
	}, nil
}

func (th *TracksHandler) resultsForLayout(trackName, layout string, hide smresults.HideFunc) ([]smresults.SessionResults, error) {
	results, err := th.env.Results().ListAll()

	if err != nil {
		return nil, err
	}

	var out []smresults.SessionResults

	for _, result := range results {
		if hide != nil && hide(result.SessionFile, result.ChampionshipID, result.RaceWeekendID) {
			continue
		}

		if result.TrackName == trackName && result.TrackConfig == layout {
			out = append(out, result)
		}
	}

	return out, nil
}

// RecordsAPI lists the lap records of a track layout: the best valid lap of every car, fastest first, and the overall
// record. Query parameters: track (required), layout (empty is the default layout), car and carClass (both repeatable;
// they restrict the records to the cars of any chosen class or any chosen car, an unknown class is a 400) and
// filterServer (restricts them to one server).
func (th *TracksHandler) RecordsAPI(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	track, layout := query.Get("track"), query.Get("layout")
	validCars := true

	for _, car := range query["car"] {
		validCars = validCars && (car == "" || plainname.Valid(car))
	}

	if !plainname.Valid(track) || (layout != "" && !plainname.Valid(layout)) || !validCars {
		http.Error(w, "track, layout and car must be valid names (track is required)", http.StatusBadRequest)
		return
	}

	filter, _, _, err := th.records.trackFilter(r)

	if smweb.FilterError(w, err) {
		return
	}

	servers := th.records.attributedServers()

	out := &APITrackRecords{
		Track:       track,
		TrackLayout: layout,
		TrackName:   api.TrackName(th.env.InstallPath(), track, layout),
		Records:     []*APIRecord{},
	}

	for _, record := range th.records.forTrack(track, layout, filter) {
		out.Records = append(out.Records, newAPIRecord(th.records, servers, record, smcontent.PrettifyName(record.Car, true)))
	}

	if len(out.Records) > 0 {
		out.Overall = out.Records[0]
	}

	w.Header().Add("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(out)
}
