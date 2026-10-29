package content

import (
	"bytes"
	"encoding/json"
	"image"
	"image/draw"
	"image/png"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/cj123/ini"
	"github.com/dimchansky/utfbom"
	"github.com/jpillora/longestcommon"
	"github.com/nfnt/resize"
	"github.com/sirupsen/logrus"
)

type Track struct {
	Name    string
	Layouts []string

	CalculatedPrettyName string

	MetaData TrackMetaData
}

const defaultTrackURL = "/static/img/no-preview-general.png"

func (t Track) GetImagePath() string {
	if len(t.Layouts) == 0 {
		return defaultTrackURL
	}

	for _, layout := range t.Layouts {
		if layout == DefaultLayoutName || layout == "" {
			return filepath.ToSlash(filepath.Join("content", "tracks", t.Name, "ui", "preview.png"))
		}
	}

	return filepath.ToSlash(filepath.Join("content", "tracks", t.Name, "ui", t.Layouts[0], "preview.png"))
}

// LoadTrackMetaDataFromName loads the Server Manager meta data (notes, download URL) of a track.
func LoadTrackMetaDataFromName(installPath, name string) (*TrackMetaData, error) {
	metaDataFile := filepath.Join(installPath, "content", "tracks", name, "ui")

	metaDataFile = filepath.Join(metaDataFile, trackMetaDataName)

	f, err := os.Open(metaDataFile)

	if err != nil && os.IsNotExist(err) {
		return &TrackMetaData{}, nil
	} else if err != nil {
		return nil, err
	}

	defer f.Close()

	var trackMetaData *TrackMetaData

	err = json.NewDecoder(utfbom.SkipOnly(f)).Decode(&trackMetaData)

	if err != nil {
		return nil, err
	}

	return trackMetaData, nil
}

func (t *Track) LoadMetaData(installPath string) error {
	trackMetaData, err := LoadTrackMetaDataFromName(installPath, t.Name)

	if err != nil {
		return err
	}

	t.MetaData = *trackMetaData

	return nil
}

func (t Track) PrettyName() string {
	if t.CalculatedPrettyName != "" {
		return t.CalculatedPrettyName
	}

	return PrettifyName(t.Name, false)
}

func (t Track) IsPaidDLC() bool {
	if _, ok := isTrackPaidDLC[t.Name]; ok {
		return isTrackPaidDLC[t.Name]
	}

	return false
}

func (t Track) IsMod() bool {
	_, ok := isTrackPaidDLC[t.Name]

	return !ok
}

// DefaultLayoutName is the name AC tooling uses for a track's default (unnamed) layout.
const DefaultLayoutName = "<default>"

func (t *Track) LayoutsCSV() string {
	if t.Layouts == nil {
		return "Default"
	}

	return strings.Join(t.Layouts, ", ")
}

// TrackLayoutURL returns the URL of the preview image of a track layout, or a default image if there is none.
func TrackLayoutURL(installPath, track, layout string) string {
	var layoutPath string

	if layout == "" || layout == DefaultLayoutName {
		layoutPath = filepath.Join("content", "tracks", track, "ui", "preview.png")
	} else {
		layoutPath = filepath.Join("content", "tracks", track, "ui", layout, "preview.png")
	}

	// look to see if the track preview image exists
	_, err := os.Stat(filepath.Join(installPath, layoutPath))

	if err != nil {
		return defaultTrackURL
	}

	return "/" + filepath.ToSlash(layoutPath)
}

const trackInfoJSONName = "ui_track.json"
const trackMetaDataName = "meta_data.json"

type TrackInfo struct {
	Name        string      `json:"name"`
	City        string      `json:"city"`
	Country     string      `json:"country"`
	Description string      `json:"description"`
	Geotags     []string    `json:"geotags"`
	Length      string      `json:"length"`
	Pitboxes    json.Number `json:"pitboxes"`
	Run         string      `json:"run"`
	Tags        []string    `json:"tags"`
	Width       string      `json:"width"`
}

type TrackMetaData struct {
	DownloadURL string `json:"downloadURL"`
	Notes       string `json:"notes"`
}

func (tmd *TrackMetaData) Save(installPath, name string) error {
	uiDirectory := filepath.Join(installPath, "content", "tracks", name, "ui")

	err := os.MkdirAll(uiDirectory, 0755)

	if err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(uiDirectory, trackMetaDataName))

	if err != nil {
		return err
	}

	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "   ")

	return enc.Encode(tmd)
}

// GetTrackInfo loads the track info (ui_track.json) of a track layout.
func GetTrackInfo(installPath, name, layout string) (*TrackInfo, error) {
	uiDataFile := filepath.Join(installPath, "content", "tracks", name, "ui")

	if layout != "" && layout != DefaultLayoutName {
		uiDataFile = filepath.Join(uiDataFile, layout)
	}

	uiDataFile = filepath.Join(uiDataFile, trackInfoJSONName)

	data, err := ioutil.ReadFile(uiDataFile)

	if err != nil {
		return nil, err
	}

	data = bytes.ReplaceAll(data, []byte("\r"), []byte(""))
	data = bytes.ReplaceAll(data, []byte("\n"), []byte(""))

	var trackInfo *TrackInfo

	err = json.NewDecoder(utfbom.SkipOnly(bytes.NewBuffer(data))).Decode(&trackInfo)

	return trackInfo, err
}

// TrackManager lists and loads the tracks in the content/tracks folder of an AC server installation.
type TrackManager struct {
	installPath string
}

// NewTrackManager creates a TrackManager for the AC server installed in installPath.
func NewTrackManager(installPath string) *TrackManager {
	return &TrackManager{installPath: installPath}
}

func (tm *TrackManager) ListTracks() ([]Track, error) {
	tracksPath := filepath.Join(tm.installPath, "content", "tracks")

	trackFiles, err := ioutil.ReadDir(tracksPath)

	if err != nil {
		return nil, err
	}

	var tracks []Track

	for _, trackFile := range trackFiles {
		track, err := tm.GetTrackFromName(trackFile.Name())

		if err != nil {
			continue
		}

		tracks = append(tracks, *track)
	}

	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].PrettyName() < tracks[j].PrettyName()
	})

	return tracks, nil
}

func (tm *TrackManager) decodeTrackImage(path string) (image.Image, error) {
	f, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	data, _, err := image.Decode(f)

	if err != nil {
		return nil, err
	}

	return data, err
}

const (
	trackMapOverlayScale = 2.0
)

func (tm *TrackManager) GetTrackImage(w io.Writer, track, layout string) (int64, error) {
	trackContentPath := filepath.Join(tm.installPath, "content", "tracks", track, "ui")
	trackMapPath := filepath.Join(tm.installPath, "content", "tracks", track, "map.png")

	if layout != "" {
		trackContentPath = filepath.Join(trackContentPath, layout)
		trackMapPath = filepath.Join(tm.installPath, "content", "tracks", track, layout, "map.png")
	}

	trackImagePath := filepath.Join(trackContentPath, "preview.png")
	trackOutlinePath := filepath.Join(trackContentPath, "outline.png")

	combinedImageMapPath := filepath.Join(trackContentPath, "server-manager_preview.png")
	combinedImageMap, err := os.Open(combinedImageMapPath)

	if err == nil {
		defer combinedImageMap.Close()

		return io.Copy(w, combinedImageMap)
	} else if !os.IsNotExist(err) {
		return -1, err
	}

	trackImage, err := tm.decodeTrackImage(trackImagePath)

	if err != nil {
		return -1, err
	}

	trackMap, err := tm.decodeTrackImage(trackMapPath)

	if os.IsNotExist(err) {
		trackMap, err = tm.decodeTrackImage(trackOutlinePath)

		if err != nil {
			return -1, err
		}
	} else if err != nil {
		return -1, err
	}

	trackImageBounds := trackImage.Bounds()
	trackMapBounds := trackMap.Bounds()

	var resizedMap image.Image

	marginX, marginY := 10, 10

	if trackMapBounds.Dx() > trackMapBounds.Dy() {
		resizedMap = resize.Resize(uint(float64(trackImageBounds.Dx())/trackMapOverlayScale), 0, trackMap, resize.Bilinear)
	} else {
		resizedMap = resize.Resize(0, uint(float64(trackImageBounds.Dy())/trackMapOverlayScale), trackMap, resize.Bilinear)
		marginX = 20
		marginY = 20
	}

	combined := image.NewRGBA(trackImageBounds)
	draw.Draw(combined, trackImageBounds, trackImage, image.Point{}, draw.Src)
	draw.Draw(combined, trackImageBounds, resizedMap, image.Pt(-trackImageBounds.Dx()+resizedMap.Bounds().Dx()+marginX, -trackImageBounds.Dy()+resizedMap.Bounds().Dy()+marginY), draw.Over)

	combinedImageMap, err = os.Create(combinedImageMapPath)

	if err != nil {
		return -1, err
	}

	defer combinedImageMap.Close()

	if err := png.Encode(combinedImageMap, combined); err != nil {
		return -1, err
	}

	if _, err := combinedImageMap.Seek(0, 0); err != nil {
		return -1, err
	}

	return io.Copy(w, combinedImageMap)
}

func (tm *TrackManager) GetTrackFromName(name string) (*Track, error) {
	tracksPath := filepath.Join(tm.installPath, "content", "tracks")
	var layouts []string

	files, err := ioutil.ReadDir(filepath.Join(tracksPath, name))

	if err != nil {
		logrus.WithError(err).Errorf("Can't read folder: %s", name)
		return nil, err
	}

	// Check for multiple layouts, if tracks have data folders in the main directory then they only have one
	if len(files) > 1 {
		for _, layout := range files {
			if layout.IsDir() {
				switch layout.Name() {
				case "data":
					layouts = append(layouts, DefaultLayoutName)
				case "ui":
					continue
				default:
					// valid layouts must contain a surfaces.ini
					_, err := os.Stat(filepath.Join(tracksPath, name, layout.Name(), "data", "surfaces.ini"))

					if os.IsNotExist(err) {
						continue
					} else if err != nil {
						return nil, err
					}

					layouts = append(layouts, layout.Name())
				}
			}
		}
	}

	track := &Track{Name: name, Layouts: layouts}

	var layoutNames []string

	for _, layout := range layouts {
		info := CachedTrackInfo(tm.installPath, track.Name, layout)

		if info == nil {
			continue
		}

		layoutNames = append(layoutNames, info.Name)
	}

	if len(layoutNames) > 0 {
		if len(layoutNames) == 1 {
			track.CalculatedPrettyName = strings.Title(layoutNames[0])
		} else {
			sort.Slice(layoutNames, func(i, j int) bool {
				return len(layoutNames[i]) < len(layoutNames[j])
			})

			commonPrefix := strings.TrimRightFunc(longestcommon.Prefix(layoutNames), func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ']' && r != ')' && r != '}'
			})

			if commonPrefix != "" {
				track.CalculatedPrettyName = strings.Title(commonPrefix)
			}
		}
	}

	return track, nil
}

func (tm *TrackManager) UpdateTrackMetadata(name string, r *http.Request) error {
	track, err := tm.GetTrackFromName(name)

	if err != nil {
		return err
	}

	track.MetaData.Notes = r.FormValue("Notes")
	track.MetaData.DownloadURL = r.FormValue("DownloadURL")

	return track.MetaData.Save(tm.installPath, name)
}

type TrackMapData struct {
	Width       float64 `ini:"WIDTH" json:"width"`
	Height      float64 `ini:"HEIGHT" json:"height"`
	Margin      float64 `ini:"MARGIN" json:"margin"`
	ScaleFactor float64 `ini:"SCALE_FACTOR" json:"scale_factor"`
	OffsetX     float64 `ini:"X_OFFSET" json:"offset_x"`
	OffsetZ     float64 `ini:"Z_OFFSET" json:"offset_y"`
	DrawingSize float64 `ini:"DRAWING_SIZE" json:"drawing_size"`
}

func LoadTrackMapData(installPath, track, trackLayout string) (*TrackMapData, error) {
	p := filepath.Join(installPath, "content", "tracks", track)

	if trackLayout != "" {
		p = filepath.Join(p, trackLayout)
	}

	p = filepath.Join(p, "data", "map.ini")

	f, err := os.Open(p)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	i, err := ini.Load(f)

	if err != nil {
		return nil, err
	}

	s, err := i.GetSection("PARAMETERS")

	if err != nil {
		return nil, err
	}

	var mapData TrackMapData

	if err := s.MapTo(&mapData); err != nil {
		return nil, err
	}

	return &mapData, nil
}

func TrackMapImageURL(track, trackLayout string) string {
	p := "/content/tracks/" + track

	if trackLayout != "" {
		p += "/" + trackLayout
	}

	return p + "/map.png"
}

func LoadTrackMapImage(installPath, track, trackLayout string) (image.Image, error) {
	p := filepath.Join(installPath, "content", "tracks", track)

	if trackLayout != "" {
		p = filepath.Join(p, trackLayout)
	}

	p = filepath.Join(p, "map.png")

	f, err := os.Open(p)

	if err != nil {
		return nil, err
	}

	defer f.Close()

	return png.Decode(f)
}

// disableDRSFile is a file with a tiny DRS zone that is too small to activate DRS in.
const disableDRSFile = `
[ZONE_0]
DETECTION=0.899
START=0
END=0.0001 
`

const (
	drsZonesFilename       = "drs_zones.ini"
	drsZonesBackupFilename = "drs_zones.ini.orig"
)

func ToggleDRSForTrack(installPath, track, layout string, drsEnabled bool) error {
	trackPath := filepath.Join(installPath, "content", "tracks", track, layout, "data")
	drsBackupFile := filepath.Join(trackPath, drsZonesBackupFilename)
	drsFile := filepath.Join(trackPath, drsZonesFilename)

	// if DRS is enabled
	if drsEnabled {
		// if the backup file exists, then rename it back into place
		if _, err := os.Stat(drsBackupFile); err == nil {
			logrus.Infof("Enabling DRS for %s (%s)", track, layout)
			err := os.Rename(drsBackupFile, drsFile)

			if err != nil && !os.IsNotExist(err) {
				return err
			}

			return nil
		} else if os.IsNotExist(err) {
			// there is no backup file. read the existing DRS file. if it's equal to disableDRSFile then we just want to delete it.
			currentDRSContents, err := ioutil.ReadFile(drsFile)

			if os.IsNotExist(err) {
				logrus.Infof("Track: %s (%s) has no drs file. DRS anywhere will be enabled.", track, layout)
				return nil
			} else if err != nil {
				return err
			}

			if string(currentDRSContents) == disableDRSFile {
				// the track has no original drs_zones.ini, just remove our file.
				logrus.Infof("Track: %s (%s) has no drs file. DRS anywhere will be enabled.", track, layout)
				err := os.Remove(drsFile)

				if err != nil && !os.IsNotExist(err) {
					return err
				}

				return nil
			}

			return nil
		} else { // err != nil
			return err
		}
	} else {
		logrus.Infof("Disabling DRS for: %s (%s)", track, layout)

		if _, err := os.Stat(drsBackupFile); os.IsNotExist(err) {
			// drs is not enabled, move the drs_zones file to backup
			if err := os.Rename(drsFile, drsBackupFile); err != nil && !os.IsNotExist(err) {
				return err
			}
		} else if err != nil {
			return err
		}

		// now write the disabled-drs file
		return ioutil.WriteFile(drsFile, []byte(disableDRSFile), 0644)
	}
}

var isTrackPaidDLC = map[string]bool{
	"ks_barcelona":        true,
	"ks_black_cat_county": false,
	"ks_brands_hatch":     true,
	"ks_drag":             false,
	"ks_highlands":        false,
	"ks_laguna_seca":      false,
	"ks_monza66":          false,
	"ks_nordschleife":     true,
	"ks_nurburgring":      false,
	"ks_red_bull_ring":    true,
	"ks_silverstone":      false,
	"ks_silverstone1967":  false,
	"ks_vallelunga":       false,
	"ks_zandvoort":        false,
	"monza":               false,
	"mugello":             false,
	"magione":             false,
	"drift":               false,
	"imola":               false,
	"spa":                 false,
	"trento-bondone":      false,
}

var (
	trackInfoCache      = make(map[string]*TrackInfo)
	trackInfoCacheMutex sync.Mutex
)

// CachedTrackInfo returns the info of a track layout, which is only read from disk once. If the info can't
// be loaded, nil is returned.
func CachedTrackInfo(installPath, track, layout string) *TrackInfo {
	trackInfoCacheMutex.Lock()
	defer trackInfoCacheMutex.Unlock()

	if t, ok := trackInfoCache[track+layout]; ok {
		return t
	}

	t, err := GetTrackInfo(installPath, track, layout)

	if err != nil {
		return nil
	}

	trackInfoCache[track+layout] = t

	return t
}

// ClearTrackInfoCache removes a track layout from the CachedTrackInfo cache, e.g. after its content changed.
func ClearTrackInfoCache(track, layout string) {
	trackInfoCacheMutex.Lock()
	defer trackInfoCacheMutex.Unlock()

	delete(trackInfoCache, track+layout)
}
