package serverconfig

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cj123/ini"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const (
	// AnyCarModel is the model of an entrant that may drive any of the cars of an event.
	AnyCarModel = "any_car_model"

	// EntryListFilename is the name of the AC server entry list file.
	EntryListFilename = "entry_list.ini"

	// DriverSwapEntrantSeparator separates the GUIDs of drivers sharing a car in a driver swap.
	DriverSwapEntrantSeparator = ";"

	// MaxEntryListSize is the maximum number of entrants in an entry list.
	MaxEntryListSize = 255
)

type EntryList map[string]*Entrant

// Write the EntryList to entry_list.ini inside configDir. setupDirectory is the AC server's setups
// directory, used to check that fixed setups exist.
func (e EntryList) Write(setupDirectory, configDir string) error {

	// belt and braces check to make sure setup file exists
	for _, entrant := range e.AsSlice() {
		if entrant.FixedSetup != "" {
			if _, err := os.Stat(filepath.Join(setupDirectory, entrant.FixedSetup)); os.IsNotExist(err) {
				return err
			}
		}
	}

	for i, entrant := range e.AsSlice() {
		entrant.PitBox = i
	}

	f := ini.NewFile([]ini.DataSource{nil}, ini.LoadOptions{
		IgnoreInlineComment: true,
	})

	// making and throwing away a default section due to the utter insanity of ini or assetto. i don't know which.
	_, err := f.NewSection("DEFAULT")

	if err != nil {
		return err
	}

	for _, v := range e.AsSlice() {
		s, err := f.NewSection(fmt.Sprintf("CAR_%d", v.PitBox))

		if err != nil {
			return err
		}

		err = s.ReflectFrom(&v)

		if err != nil {
			return err
		}
	}

	return f.SaveTo(filepath.Join(configDir, EntryListFilename))
}

// ReadString returns the contents of entry_list.ini inside configDir.
func (e EntryList) ReadString(configDir string) (string, error) {
	content, err := ioutil.ReadFile(filepath.Join(configDir, EntryListFilename))
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// Add an Entrant to the EntryList
func (e EntryList) AddToBackOfGrid(entrant *Entrant) {
	e.AddInPitBox(entrant, len(e))
}

// AddInPitBox adds an Entrant in a specific pitbox - overwriting any entrant that was in that pitbox previously.
func (e EntryList) AddInPitBox(entrant *Entrant, pitBox int) {
	pitBoxKey := fmt.Sprintf("CAR_%d", pitBox)

	if existingEntrant, ok := e[pitBoxKey]; ok {
		logrus.Warnf("Car already present in pitbox: %d! Driver: %s (%s) in %s will be overwritten!", pitBox, existingEntrant.Name, existingEntrant.GUID, existingEntrant.Model)
	}

	entrant.PitBox = pitBox
	e[pitBoxKey] = entrant
}

// Remove an Entrant from the EntryList
func (e EntryList) Delete(entrant *Entrant) {
	for k, v := range e {
		if v == entrant {
			delete(e, k)
			return
		}
	}
}

func (e EntryList) AsSlice() []*Entrant {
	var entrants []*Entrant

	for _, x := range e {
		entrants = append(entrants, x)
	}

	// note: pitbox sorting here is crucial
	sort.Slice(entrants, func(i, j int) bool {
		return entrants[i].PitBox < entrants[j].PitBox
	})

	return entrants
}

func (e EntryList) AlphaSlice() []*Entrant {
	var entrants []*Entrant

	for _, x := range e {
		entrants = append(entrants, x)
	}

	sort.Slice(entrants, func(i, j int) bool {
		return entrants[i].Name < entrants[j].Name
	})

	return entrants
}

func (e EntryList) PrettyList() []*Entrant {
	var entrants []*Entrant

	numOpenSlots := 0

	for _, x := range e {
		if x.GUID == "" {
			numOpenSlots++
			continue
		}

		if x.Model == AnyCarModel {
			continue
		}

		entrants = append(entrants, x)
	}

	sort.Slice(entrants, func(i, j int) bool {
		return entrants[i].Name < entrants[j].Name
	})

	entrants = append(entrants, &Entrant{
		Name: fmt.Sprintf("%d open slots", numOpenSlots),
		GUID: "OPEN_SLOTS",
	})

	return entrants
}

// Entrants returns a comma separated list of the entrants' names, each formatted by formatName,
// followed by the number of open slots.
func (e EntryList) Entrants(formatName func(string) string) string {
	var entrants []string

	numOpenSlots := 0

	for _, x := range e {
		if x.Name == "" {
			numOpenSlots++
		} else {
			entrants = append(entrants, formatName(x.Name))
		}
	}

	if numOpenSlots > 0 {
		entrants = append(entrants, fmt.Sprintf("%d open slots", numOpenSlots))
	}

	return strings.Join(entrants, ", ")
}

func (e EntryList) FindEntrantByInternalUUID(internalUUID uuid.UUID) *Entrant {
	for _, entrant := range e {
		if entrant.InternalUUID == internalUUID {
			return entrant
		}
	}

	return &Entrant{}
}

// CarIDs returns a unique list of car IDs used in the EntryList
func (e EntryList) CarIDs() []string {
	cars := make(map[string]bool)

	for _, entrant := range e {
		cars[entrant.Model] = true
	}

	var out []string

	for car := range cars {
		out = append(out, car)
	}

	return out
}

// returns the greatest ballast set on any entrant
func (e EntryList) FindGreatestBallast() int {
	var greatest int

	for _, entrant := range e {
		if entrant.Ballast > greatest {
			greatest = entrant.Ballast
		}
	}

	return greatest
}

func NewEntrant() *Entrant {
	return &Entrant{
		InternalUUID: uuid.New(),
	}
}

type Entrant struct {
	InternalUUID uuid.UUID `ini:"-"`
	PitBox       int       `ini:"-"`

	Name string `ini:"DRIVERNAME"`
	Team string `ini:"TEAM"`
	GUID string `ini:"GUID"`

	Model string `ini:"MODEL"`
	Skin  string `ini:"SKIN"`

	Ballast       int    `ini:"BALLAST"`
	SpectatorMode int    `ini:"SPECTATOR_MODE"`
	Restrictor    int    `ini:"RESTRICTOR"`
	FixedSetup    string `ini:"FIXED_SETUP"`

	TransferTeamPoints bool `ini:"-" json:"-"`
	OverwriteAllEvents bool `ini:"-" json:"-"`
	IsPlaceHolder      bool `ini:"-"`
}

func (e Entrant) ID() string {
	if e.GUID != "" {
		return e.GUID
	}

	return e.Name
}

func (e *Entrant) OverwriteProperties(other *Entrant) {
	e.FixedSetup = other.FixedSetup
	e.Restrictor = other.Restrictor
	e.SpectatorMode = other.SpectatorMode
	e.Ballast = other.Ballast
	e.Skin = other.Skin
	e.PitBox = other.PitBox
}

func (e *Entrant) SwapProperties(other *Entrant, entrantRemainedInClass bool) {
	if entrantRemainedInClass {
		e.Model, other.Model = other.Model, e.Model
		e.Skin, other.Skin = other.Skin, e.Skin
		e.FixedSetup, other.FixedSetup = other.FixedSetup, e.FixedSetup
		e.Restrictor, other.Restrictor = other.Restrictor, e.Restrictor
		e.Ballast, other.Ballast = other.Ballast, e.Ballast
	}

	e.Team, other.Team = other.Team, e.Team
	e.InternalUUID, other.InternalUUID = other.InternalUUID, e.InternalUUID
	e.PitBox, other.PitBox = other.PitBox, e.PitBox
}

var guidCleanupRegex = regexp.MustCompile(`[^0-9]+`)

func CleanGUIDs(guids []string) []string {
	var cleaned []string

	for _, guid := range guids {
		g := guidCleanupRegex.ReplaceAllLiteralString(guid, "")

		if len(g) > 0 {
			cleaned = append(cleaned, g)
		}
	}

	return cleaned
}

// NormaliseEntrantGUID takes a guid which may have driverSwapEntrantSeparators in it,
// sorts all GUIDs in the string and then rejoins them by DriverSwapEntrantSeparator
func NormaliseEntrantGUID(guid string) string {
	split := CleanGUIDs(strings.Split(guid, DriverSwapEntrantSeparator))

	sort.Strings(split)

	return strings.Join(split, DriverSwapEntrantSeparator)
}

// NormaliseEntrantGUIDs takes a list of guids, sorts them and joins them by DriverSwapEntrantSeparator
func NormaliseEntrantGUIDs(guids []string) string {
	guids = CleanGUIDs(guids)

	sort.Strings(guids)

	return strings.Join(guids, DriverSwapEntrantSeparator)
}
