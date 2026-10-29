package contenthttp

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi"
	"github.com/sirupsen/logrus"

	smcontent "github.com/wernersbacher/assetto-server-manager/internal/content"
	"github.com/wernersbacher/assetto-server-manager/internal/plainname"
	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// TracksHandler serves the track list, the track details, the track metadata and the track previews.
type TracksHandler struct {
	*smweb.BaseHandler

	trackManager *smcontent.TrackManager
	env          *serverenv.Env

	// noPreview returns the image which is served for tracks without a preview.
	noPreview func() []byte
}

func NewTracksHandler(baseHandler *smweb.BaseHandler, trackManager *smcontent.TrackManager, env *serverenv.Env, noPreview func() []byte) *TracksHandler {
	return &TracksHandler{
		BaseHandler:  baseHandler,
		trackManager: trackManager,
		env:          env,
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
		smweb.AddErrorFlash(w, r, "couldn't get track list")
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
		smweb.AddFlash(w, r, "Track successfully deleted!")
	} else {
		// inform track wasn't found
		smweb.AddErrorFlash(w, r, "Sorry, track could not be deleted.")
	}

	http.Redirect(w, r, "/tracks", http.StatusFound)
}

func (th *TracksHandler) View(w http.ResponseWriter, r *http.Request) {
	trackName := chi.URLParam(r, "track_id")

	if !plainname.Valid(trackName) {
		http.NotFound(w, r)
		return
	}

	templateParams, err := th.loadTrackDetailsForTemplate(trackName)

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

	smweb.AddFlash(w, r, "Track metadata updated successfully!")
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
}

func (th *TracksHandler) loadTrackDetailsForTemplate(trackName string) (*trackDetailsTemplateVars, error) {
	trackInfoMap := make(map[string]*smcontent.TrackInfo)
	resultsMap := make(map[string][]smresults.SessionResults)

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

		results, err := th.resultsForLayout(track.Name, layout)

		if err != nil {
			logrus.WithError(err).Errorf("Couldn't load results for layout: %s, track: %s", layout, track.Name)
			continue
		}

		resultsMap[layout] = results
	}

	return &trackDetailsTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{},
		Track:            track,
		TrackInfo:        trackInfoMap,
		Results:          resultsMap,
	}, nil
}

func (th *TracksHandler) resultsForLayout(trackName, layout string) ([]smresults.SessionResults, error) {
	results, err := th.env.Results().ListAll()

	if err != nil {
		return nil, err
	}

	var out []smresults.SessionResults

	for _, result := range results {
		if result.TrackName == trackName && result.TrackConfig == layout {
			out = append(out, result)
		}
	}

	return out, nil
}
