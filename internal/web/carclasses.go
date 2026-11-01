package web

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/sirupsen/logrus"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// CarClasses gives the global car classes. carclasses.Manager implements it; handler packages only know this
// interface because the package that owns the classes sits above them. Get returns smmodel.ErrCarClassNotFound for an
// unknown class or an ID that is no UUID.
type CarClasses interface {
	List() ([]*smmodel.CarClass, error)
	Get(id string) (*smmodel.CarClass, error)
}

// UnknownCarClassError is the error of a carClass parameter that names no car class. It is a client error (400).
type UnknownCarClassError struct {
	ID string
}

func (e *UnknownCarClassError) Error() string {
	return fmt.Sprintf("carClass %q is not a known car class", e.ID)
}

// ErrCarClassesUnavailable is returned if the car classes cannot be loaded. It is not the fault of the client.
var ErrCarClassesUnavailable = errors.New("car class could not be loaded")

// CarClassSelection is the car classes a request asked for.
type CarClassSelection struct {
	// IDs are the canonical IDs of the classes, without duplicates, in the order asked for.
	IDs []string

	// Cars are the cars of the classes, without duplicates.
	Cars []string
}

// ResolveCarClasses looks up the carClass parameters. Empty values are ignored. An unknown class (or any ID if
// classes is nil: there are no classes) is an *UnknownCarClassError, never an empty or an unfiltered selection.
func ResolveCarClasses(classes CarClasses, ids []string) (*CarClassSelection, error) {
	selection := &CarClassSelection{}

	for _, id := range ids {
		if id == "" {
			continue
		}

		var class *smmodel.CarClass

		if classes != nil {
			var err error

			class, err = classes.Get(id)

			if err != nil && !errors.Is(err, smmodel.ErrCarClassNotFound) {
				logrus.WithError(err).Error("couldn't load car class")

				return nil, ErrCarClassesUnavailable
			}
		}

		if class == nil {
			return nil, &UnknownCarClassError{ID: id}
		}

		selection.IDs = appendMissing(selection.IDs, class.ID.String())
		selection.Cars = appendMissing(selection.Cars, class.Cars...)
	}

	return selection, nil
}

// UnionCars returns the cars and the cars of the classes: a car counts if it is one of the cars or in a class.
func (s *CarClassSelection) UnionCars(cars []string) []string {
	return appendMissing(append([]string(nil), cars...), s.Cars...)
}

// IsUnknownCarClass tells if the error of ResolveCarClasses is the fault of the request.
func IsUnknownCarClass(err error) bool {
	var unknown *UnknownCarClassError

	return errors.As(err, &unknown)
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}

	return false
}

func appendMissing(list []string, values ...string) []string {
	for _, v := range values {
		if !containsString(list, v) {
			list = append(list, v)
		}
	}

	return list
}

// CarClassOption is an entry of the car class checkboxes of a filter form.
type CarClassOption struct {
	ID       string
	Name     string
	Selected bool
}

// ListCarClasses lists the car classes, nil if there are none or they cannot be loaded (the pages work without them).
func ListCarClasses(classes CarClasses) []*smmodel.CarClass {
	if classes == nil {
		return nil
	}

	list, err := classes.List()

	if err != nil {
		logrus.WithError(err).Error("couldn't list car classes")

		return nil
	}

	return list
}

// CarClassOptions lists the car classes for a filter form; selected are the canonical IDs of the chosen ones.
func CarClassOptions(classes CarClasses, selected []string) []CarClassOption {
	var options []CarClassOption

	for _, class := range ListCarClasses(classes) {
		id := class.ID.String()

		options = append(options, CarClassOption{ID: id, Name: class.Name, Selected: containsString(selected, id)})
	}

	return options
}

// FilterError answers a request with an invalid filter (see ResolveCarClasses) and reports whether it did: 400 for an
// unknown class or any other client error, 500 if the classes could not be loaded. It does nothing for a nil error.
func FilterError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}

	status := http.StatusBadRequest

	if errors.Is(err, ErrCarClassesUnavailable) {
		status = http.StatusInternalServerError
	}

	http.Error(w, err.Error(), status)

	return true
}
