package servermanager

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/search/query"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

const searchPageSize = 50

// CreateSearchIndex builds a search index for the cars
func (cm *CarManager) CreateOrOpenSearchIndex() error {
	cm.searchMutex.Lock()
	indexPath := filepath.Join(ServerInstallPath, "search-index", "cars")

	var err error

	cm.carIndex, err = bleve.Open(indexPath)
	cm.searchMutex.Unlock()

	if err == bleve.ErrorIndexPathDoesNotExist {
		logrus.Infof("Creating car search index")
		indexMapping := bleve.NewIndexMapping()
		cm.carIndex, err = bleve.New(indexPath, indexMapping)

		if err != nil {
			return err
		}

		err = cm.IndexAllCars()

		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if cm.watchFilesystemForCarChanges {
		go panicCapture(func() {
			err := cm.watchForCarChanges()

			if err != nil {
				logrus.WithError(err).Error("Could not watch for changes in the content/cars directory")
			}
		})
	}

	return nil
}

func (cm *CarManager) UpdateTyres(car *Car) error {
	if car.Name == "" {
		return nil
	}

	cm.tyreUpdateMutex.Lock()
	defer cm.tyreUpdateMutex.Unlock()

	carPath := filepath.Join(ServerInstallPath, "content", "cars", car.Name)
	acdPath := filepath.Join(carPath, "data.acd")

	b, err := ioutil.ReadFile(acdPath)

	if err == nil {
		return addTyresFromDataACD(acdPath, b)
	} else if !os.IsNotExist(err) {
		return err
	}

	tyresIniPath := filepath.Join(carPath, "data", "tyres.ini")

	b, err = ioutil.ReadFile(tyresIniPath)

	if err != nil {
		return err
	}

	return addTyresFromTyresIni(tyresIniPath, b)
}

// IndexCar indexes an individual car.
func (cm *CarManager) IndexCar(car *Car) error {
	carNameCache.add(car)

	if err := cm.UpdateTyres(car); err != nil {
		logrus.WithError(err).Errorf("Could not update tyres for car: %s", car.Name)
	}

	return cm.carIndex.Index(car.Name, car.Details)
}

// DeIndexCar removes a car from the index.
func (cm *CarManager) DeIndexCar(name string) error {
	carNameCache.remove(name)

	return cm.carIndex.Delete(name)
}

// IndexAllCars loads all current cars and adds them to the search index
func (cm *CarManager) IndexAllCars() error {
	logrus.Infof("Building search index for all cars")
	started := time.Now()

	results, _, err := cm.Search(context.Background(), "", 0, 100000)

	if err == nil {
		errs, _ := errgroup.WithContext(context.Background())

		for _, result := range results.Hits {
			result := result

			errs.Go(func() error {
				return cm.DeIndexCar(result.ID)
			})
		}

		if err := errs.Wait(); err != nil {
			return err
		}
	} else {
		logrus.WithError(err).Warnf("could not de-index cars")
	}

	cars, err := cm.ListCars()

	if err != nil {
		return err
	}

	errs, _ := errgroup.WithContext(context.Background())

	for _, car := range cars {
		car := car

		errs.Go(func() error {
			return cm.IndexCar(car)
		})
	}

	if err := errs.Wait(); err != nil {
		return err
	}

	logrus.Infof("Search index build is complete (took: %s)", time.Since(started).String())

	return nil
}

var (
	positiveCarTypeRegex = regexp.MustCompile(`\+(mod|dlc|stock)`)
	negativeCarTypeRegex = regexp.MustCompile(`-(mod|dlc|stock)`)
)

func (cm *CarManager) rebuildTerm(term string) string {
	// bleve only allows searching for true/false via the ugly terms
	// e.g. dlc:T* - make these a bit more user friendly (e.g. +dlc)
	term = positiveCarTypeRegex.ReplaceAllString(term, "$1:T*")
	term = negativeCarTypeRegex.ReplaceAllString(term, "$1:F*")

	return term
}

// Search looks for cars in the search index.
func (cm *CarManager) Search(ctx context.Context, term string, from, size int) (*bleve.SearchResult, Cars, error) {
	cm.searchMutex.Lock()
	defer cm.searchMutex.Unlock()

	var q query.Query

	term = cm.rebuildTerm(term)

	if term == "" {
		q = bleve.NewMatchAllQuery()
	} else {
		q = bleve.NewQueryStringQuery(term)
	}

	request := bleve.NewSearchRequestOptions(q, size, from, false)
	results, err := cm.carIndex.SearchInContext(ctx, request)

	if err != nil {
		return nil, nil, err
	}

	var cars Cars

	for _, hit := range results.Hits {
		if hit.ID == "cars" {
			continue
		}

		car, err := cm.LoadCar(hit.ID, nil)

		if err != nil {
			return nil, nil, errors.Wrap(err, hit.ID)
		}

		cars = append(cars, car)
	}

	return results, cars, nil
}
