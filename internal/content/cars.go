package content

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/cj123/watcher"
	"github.com/sirupsen/logrus"
)

// CarManager lists, loads, searches and modifies the cars in the content/cars folder of an AC server installation.
type CarManager struct {
	installPath string

	carIndex                     bleve.Index
	watchFilesystemForCarChanges bool

	searchMutex     sync.Mutex
	tyreUpdateMutex sync.Mutex

	// PanicCapture, if set, wraps the background goroutines of the CarManager so that panics can be recovered.
	PanicCapture func(fn func())
}

// NewCarManager creates a CarManager for the AC server installed in installPath.
func NewCarManager(installPath string, watchForCarChanges, useCarNameCache bool) *CarManager {
	cm := &CarManager{installPath: installPath, watchFilesystemForCarChanges: watchForCarChanges}

	if useCarNameCache {
		cm.initCarNames()
	}

	return cm
}

func (cm *CarManager) capturePanic(fn func()) {
	if cm.PanicCapture != nil {
		cm.PanicCapture(fn)
		return
	}

	fn()
}

type carNames map[string]string

var (
	// carNameCache provides a map of car key -> actual name of a car
	// this can be used to improve the accuracy of car naming in templates.
	carNameCache carNames

	carNameMutex sync.RWMutex
)

// adds the name of a car to the car details cache.
func (c carNames) add(car *Car) {
	if c == nil {
		return
	}

	carNameMutex.Lock()
	defer carNameMutex.Unlock()

	if car.Details.Name != "" {
		carNameCache[car.Name] = car.Details.Name
	}
}

// get a car name from the cache, if possible.
// if cache is not enabled, false is always returned.
func (c carNames) get(car string) (string, bool) {
	if c == nil {
		return "", false
	}

	carNameMutex.RLock()
	defer carNameMutex.RUnlock()

	name, ok := c[car]

	return name, ok
}

// removes a car name from the cache.
func (c carNames) remove(car string) {
	if c == nil {
		return
	}

	carNameMutex.Lock()
	defer carNameMutex.Unlock()

	delete(carNameCache, car)
}

func (cm *CarManager) initCarNames() {
	carNameCache = make(carNames)

	// including the vanilla cars: old results and entry lists still name them
	cars, err := cm.listCars(true)

	if err != nil {
		return
	}

	for _, car := range cars {
		carNameCache.add(car)
	}
}

// watchForChanges looks for created/removed files in the cars folder and (de-)indexes them as necessary
func (cm *CarManager) watchForCarChanges() error {
	w := watcher.New()

	err := w.Add(filepath.Join(cm.installPath, "content", "cars"))

	if err != nil {
		return err
	}

	w.SetMaxEvents(0)
	w.FilterOps(watcher.Create, watcher.Remove)
	w.AddFilterHook(func(info os.FileInfo, fullPath string) error {
		if info.IsDir() && info.Name() != "cars" {
			split := strings.Split(fullPath, fmt.Sprintf("%c", os.PathSeparator))

			if len(split) > 0 && split[len(split)-2] == "cars" {
				return nil // only fire the event for the car folder itself
			}
		}

		return watcher.ErrSkip
	})

	go cm.capturePanic(func() {
		for {
			select {
			case event := <-w.Event:
				var err error
				var carName string

				switch event.Op {
				case watcher.Create, watcher.Write:
					carName = filepath.Base(event.Path)
					logrus.Infof("Indexing car: %s", carName)
					car, err := cm.LoadCar(carName, nil)

					if err != nil {
						logrus.WithError(err).Errorf("Could not find car to index: %s", carName)
						continue
					}

					err = cm.IndexCar(car)

					if err != nil {
						logrus.WithError(err).Errorf("Could not index car: %s", carName)
						continue
					}
				case watcher.Remove:
					carName = filepath.Base(event.OldPath)
					logrus.Infof("De-indexing car: %s", carName)
					err = cm.DeIndexCar(carName)
				}

				if err != nil {
					logrus.WithError(err).Errorf("Could not update index for car: %s", carName)
					continue
				}
			case err := <-w.Error:
				logrus.WithError(err).Error("Car content watcher error")
				continue
			case <-w.Closed:
				return
			}
		}
	})

	return w.Start(time.Second * 15)
}

// ListCars lists the cars that were uploaded. The cars that came with the acServer install are left out.
func (cm *CarManager) ListCars() (Cars, error) {
	return cm.listCars(false)
}

func (cm *CarManager) listCars(withVanilla bool) (Cars, error) {
	var cars Cars

	vanilla := currentVanilla(cm.installPath)

	carFiles, err := ioutil.ReadDir(filepath.Join(cm.installPath, "content", "cars"))

	if err != nil {
		return nil, err
	}

	tyres, err := ListTyres(cm.installPath)

	if err != nil {
		return nil, err
	}

	for _, carFile := range carFiles {
		if !carFile.IsDir() || (!withVanilla && vanilla.has("cars", carFile.Name())) {
			continue
		}

		car, err := cm.LoadCar(carFile.Name(), tyres)

		if err != nil && os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}

		cars = append(cars, car)
	}

	sort.Slice(cars, func(i, j int) bool {
		return cars[i].PrettyName() < cars[j].PrettyName()
	})

	return cars, nil
}

// LoadCar reads a car from the content folder on the filesystem
func (cm *CarManager) LoadCar(name string, tyres Tyres) (*Car, error) {
	carDirectory := filepath.Join(cm.installPath, "content", "cars", name)
	skinFiles, err := ioutil.ReadDir(filepath.Join(carDirectory, "skins"))

	var skins []string

	if err == nil {
		for _, skinFile := range skinFiles {
			if !skinFile.IsDir() {
				continue
			}

			skins = append(skins, skinFile.Name())
		}
	} else {
		if os.IsNotExist(err) {
			if err := os.Mkdir(filepath.Join(carDirectory, "skins"), 0755); err != nil && !os.IsNotExist(err) {
				logrus.WithError(err).Warnf("Could not create skins directory for car: %s", name)
			}
		} else {
			logrus.WithError(err).Warnf("Could not load skins for car: %s", name)
		}
	}

	carDetails := CarDetails{}

	if err := carDetails.Load(cm.installPath, name); err != nil {
		if !os.IsNotExist(err) {
			logrus.WithError(err).Errorf("could not parse car details json for: %s (likely this is invalid/malformed JSON). falling back to empty car details", name)
		}

		// the car details don't exist or can't be loaded, just create some fake ones.
		carDetails.Name = PrettifyName(name, true)
	}

	carDetails.Key = name
	carDetails.PrettifiedKey = PrettifyName(name, true)

	return &Car{
		Name:    name,
		Skins:   skins,
		Tyres:   tyres[name],
		Details: carDetails,
	}, nil
}

func (cm *CarManager) RandomSkin(model string) string {
	car, err := cm.LoadCar(model, nil)

	switch {
	case err != nil:
		logrus.WithError(err).Errorf("Could not load car %s. No skin will be specified", model)
		return ""
	case len(car.Skins) == 0:
		logrus.Warnf("Car %s has no skins uploaded. No skin will be specified", model)
		return ""
	default:
		return car.Skins[rand.Intn(len(car.Skins))]
	}
}

// DeleteCar removes a car from the file system and search index.
func (cm *CarManager) DeleteCar(carName string) error {
	carsPath := filepath.Join(cm.installPath, "content", "cars")

	existingCars, err := cm.ListCars()

	if err != nil {
		return err
	}

	for _, car := range existingCars {
		if car.Name != carName {
			continue
		}

		err := os.RemoveAll(filepath.Join(carsPath, carName))

		if err != nil {
			return err
		}

		break
	}

	return cm.DeIndexCar(carName)
}

func (cm *CarManager) AddTag(carName, tag string) error {
	car, err := cm.LoadCar(carName, nil)

	if err != nil {
		return err
	}

	car.Details.AddTag(tag)

	return cm.SaveCarDetails(carName, car)
}

func (cm *CarManager) DelTag(carName, tag string) error {
	car, err := cm.LoadCar(carName, nil)

	if err != nil {
		return err
	}

	car.Details.DelTag(tag)

	return cm.SaveCarDetails(carName, car)
}

// SaveCarDetails saves a car's details, and indexes that car.
func (cm *CarManager) SaveCarDetails(carName string, car *Car) error {
	if err := car.Details.Save(cm.installPath, carName); err != nil {
		return err
	}

	return cm.IndexCar(car)
}

func (cm *CarManager) UpdateCarMetadata(carName string, r *http.Request) error {
	car, err := cm.LoadCar(carName, nil)

	if err != nil {
		return err
	}

	car.Details.Notes = r.FormValue("Notes")
	car.Details.DownloadURL = r.FormValue("DownloadURL")

	return car.Details.Save(cm.installPath, carName)
}

func (cm *CarManager) DeleteSkin(car, skin string) error {
	return os.RemoveAll(filepath.Join(cm.installPath, "content", "cars", car, "skins", skin))
}
