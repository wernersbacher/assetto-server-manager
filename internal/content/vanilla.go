package content

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// vanillaFile is the marker file (below content/) that records the cars and tracks that came with the acServer
// install. They only hold data, not what a driver would expect of uploaded content, so the UI hides them until the
// same name is uploaded.
const vanillaFile = ".acpitwall-vanilla.json"

var vanillaMutex sync.Mutex

type vanillaContent struct {
	Cars   []string `json:"cars"`
	Tracks []string `json:"tracks"`
}

func (v *vanillaContent) names(kind string) *[]string {
	if kind == "tracks" {
		return &v.Tracks
	}

	return &v.Cars
}

func (v *vanillaContent) has(kind, name string) bool {
	for _, n := range *v.names(kind) {
		if n == name {
			return true
		}
	}

	return false
}

func loadVanilla(installPath string) *vanillaContent {
	v := &vanillaContent{}

	data, err := ioutil.ReadFile(filepath.Join(installPath, "content", vanillaFile))

	if err != nil {
		return v // no marker (install from before this feature): nothing is hidden
	}

	if err := json.Unmarshal(data, v); err != nil {
		return &vanillaContent{}
	}

	return v
}

func saveVanilla(installPath string, v *vanillaContent) error {
	sort.Strings(v.Cars)
	sort.Strings(v.Tracks)

	data, err := json.MarshalIndent(v, "", "  ")

	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath.Join(installPath, "content", vanillaFile), data, 0644)
}

func contentDirs(installPath, kind string) map[string]bool {
	dirs := make(map[string]bool)

	entries, err := ioutil.ReadDir(filepath.Join(installPath, "content", kind))

	if err != nil {
		return dirs
	}

	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = true
		}
	}

	return dirs
}

// ContentSnapshot is the list of car and track folders of an install at one point in time.
type ContentSnapshot struct {
	cars, tracks map[string]bool
}

// TakeContentSnapshot lists the car and track folders in installPath. Take one before running steamcmd and give it to
// MarkVanillaContent afterwards.
func TakeContentSnapshot(installPath string) ContentSnapshot {
	return ContentSnapshot{cars: contentDirs(installPath, "cars"), tracks: contentDirs(installPath, "tracks")}
}

// MarkVanillaContent records the cars and tracks that appeared since before was taken (the content of the acServer
// install) as vanilla, so ListCars and ListTracks no longer show them. Earlier entries stay, so content that was
// uploaded before an update is not hidden by it.
func MarkVanillaContent(installPath string, before ContentSnapshot) error {
	vanillaMutex.Lock()
	defer vanillaMutex.Unlock()

	v := loadVanilla(installPath)

	for name := range contentDirs(installPath, "cars") {
		if !before.cars[name] && !v.has("cars", name) {
			v.Cars = append(v.Cars, name)
		}
	}

	for name := range contentDirs(installPath, "tracks") {
		if !before.tracks[name] && !v.has("tracks", name) {
			v.Tracks = append(v.Tracks, name)
		}
	}

	if err := os.MkdirAll(filepath.Join(installPath, "content"), 0755); err != nil {
		return err
	}

	return saveVanilla(installPath, v)
}

// ForgetVanillaCar makes a car count as uploaded. Call it when files for the car were uploaded.
func ForgetVanillaCar(installPath, name string) error {
	return forgetVanilla(installPath, "cars", name)
}

// ForgetVanillaTrack makes a track count as uploaded. Call it when files for the track were uploaded.
func ForgetVanillaTrack(installPath, name string) error {
	return forgetVanilla(installPath, "tracks", name)
}

func forgetVanilla(installPath, kind, name string) error {
	vanillaMutex.Lock()
	defer vanillaMutex.Unlock()

	v := loadVanilla(installPath)

	if !v.has(kind, name) {
		return nil
	}

	names := v.names(kind)
	kept := (*names)[:0]

	for _, n := range *names {
		if n != name {
			kept = append(kept, n)
		}
	}

	*names = kept

	return saveVanilla(installPath, v)
}

// currentVanilla reads the marker once, so a listing does not read the file per entry.
func currentVanilla(installPath string) *vanillaContent {
	vanillaMutex.Lock()
	defer vanillaMutex.Unlock()

	return loadVanilla(installPath)
}
