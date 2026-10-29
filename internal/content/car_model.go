package content

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/dimchansky/utfbom"
)

type Car struct {
	Name    string
	Skins   []string
	Tyres   map[string]string
	Details CarDetails
}

// PrettyName is the readable name of the car, taking the car name cache into account.
func (c Car) PrettyName() string {
	return PrettifyName(c.Name, true)
}

func (c Car) IsPaidDLC() bool {
	if _, ok := isCarPaidDLC[c.Name]; ok {
		return isCarPaidDLC[c.Name]
	}

	return false
}

func (c Car) IsMod() bool {
	_, ok := isCarPaidDLC[c.Name]

	return !ok
}

type Cars []*Car

func (cs Cars) AsMap() map[string][]string {
	out := make(map[string][]string)

	for _, car := range cs {
		out[car.Name] = car.Skins
	}

	return out
}

type CarDetails struct {
	Author        string          `json:"author"`
	Brand         string          `json:"brand"`
	Class         string          `json:"class"`
	Country       string          `json:"country"`
	Description   string          `json:"description"`
	Name          string          `json:"name"`
	PowerCurve    [][]json.Number `json:"powerCurve"`
	Specs         CarSpecs        `json:"specs"`
	SpecsNumeric  CarSpecsNumeric `json:"spec"`
	Tags          []string        `json:"tags"`
	TorqueCurve   [][]json.Number `json:"torqueCurve"`
	URL           string          `json:"url"`
	Version       string          `json:"version"`
	Year          ShouldBeAnInt   `json:"year"`
	IsStock       bool            `json:"stock"`
	IsDLC         bool            `json:"dlc"`
	IsMod         bool            `json:"mod"`
	Key           string          `json:"key"`
	PrettifiedKey string          `json:"prettified_key"`

	DownloadURL string `json:"downloadURL"`
	Notes       string `json:"notes"`
}

// ShouldBeAnInt can be used in JSON struct definitions in places where the value provided should be an int, but isn't.
type ShouldBeAnInt int

func (i *ShouldBeAnInt) UnmarshalJSON(b []byte) error {
	var number int

	err := json.Unmarshal(b, &number)

	if err != nil {
		var str string

		err := json.Unmarshal(b, &str)

		if err != nil {
			return err
		}

		*i = ShouldBeAnInt(formValueAsInt(str))
	} else {
		*i = ShouldBeAnInt(number)
	}

	return nil
}

func (cd *CarDetails) AddTag(name string) {
	for _, tag := range cd.Tags {
		if tag == name {
			// tag exists
			return
		}
	}

	cd.Tags = append(cd.Tags, name)
}

func (cd *CarDetails) DelTag(name string) {
	deleteIndex := -1

	for index, tag := range cd.Tags {
		if tag == name {
			deleteIndex = index
		}
	}

	if deleteIndex == -1 {
		return
	}

	cd.Tags = append(cd.Tags[:deleteIndex], cd.Tags[deleteIndex+1:]...)
}

func (cd *CarDetails) Save(installPath, carName string) error {
	uiDirectory := filepath.Join(installPath, "content", "cars", carName, "ui")

	err := os.MkdirAll(uiDirectory, 0755)

	if err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(uiDirectory, "ui_car.json"))

	if err != nil {
		return err
	}

	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "   ")

	return enc.Encode(cd)
}

func (cd *CarDetails) Load(installPath, carName string) error {
	f, err := os.Open(filepath.Join(installPath, "content", "cars", carName, "ui", "ui_car.json"))

	if err != nil {
		return err
	}

	defer f.Close()

	carDetailsBytes, err := ioutil.ReadAll(utfbom.SkipOnly(f))

	if err != nil {
		return err
	}

	carDetailsBytes = regexp.MustCompile(`\t*\r*\n*`).ReplaceAll(carDetailsBytes, []byte(""))

	err = json.Unmarshal(carDetailsBytes, &cd)

	if err != nil {
		return err
	}

	cd.SpecsNumeric = cd.Specs.Numeric()

	isDLC, isStock := isCarPaidDLC[carName]
	cd.IsStock = isStock
	cd.IsDLC = isDLC
	cd.IsMod = !isStock && !isDLC

	return nil
}

type CarSpecs struct {
	Acceleration string `json:"acceleration"`
	BHP          string `json:"bhp"`
	PWRatio      string `json:"pwratio"`
	TopSpeed     string `json:"topspeed"`
	Torque       string `json:"torque"`
	Weight       string `json:"weight"`
}

type CarSpecsNumeric struct {
	Acceleration int `json:"acceleration"`
	BHP          int `json:"bhp"`
	PWRatio      int `json:"pwratio"`
	TopSpeed     int `json:"topspeed"`
	Torque       int `json:"torque"`
	Weight       int `json:"weight"`
}

var keepNumericRegex = regexp.MustCompile(`[0-9]+`)

func toNumber(str string) int {
	str = keepNumericRegex.FindString(str)

	return formValueAsInt(str)
}

func (cs CarSpecs) Numeric() CarSpecsNumeric {
	return CarSpecsNumeric{
		Acceleration: toNumber(cs.Acceleration),
		BHP:          toNumber(cs.BHP),
		PWRatio:      toNumber(cs.PWRatio),
		TopSpeed:     toNumber(cs.TopSpeed),
		Torque:       toNumber(cs.Torque),
		Weight:       toNumber(cs.Weight),
	}
}

// formValueAsInt parses val as an int, "on" (checkbox) counts as 1. Anything unparseable is 0.
func formValueAsInt(val string) int {
	if val == "on" {
		return 1
	}

	i, err := strconv.ParseInt(val, 10, 0)

	if err != nil {
		return 0
	}

	return int(i)
}
