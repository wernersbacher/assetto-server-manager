package model

import (
	"encoding/gob"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

func init() {
	gob.RegisterName("github.com/wernersbacher/ACPitwall.CarClass", CarClass{})
}

var (
	ErrCarClassNotFound     = errors.New("servermanager: car class not found")
	ErrCarClassNameEmpty    = errors.New("servermanager: car class name must not be empty")
	ErrCarClassNameTaken    = errors.New("servermanager: car class name is already taken")
	ErrCarClassNoCars       = errors.New("servermanager: car class needs at least one car")
	ErrCarClassEmptyCar     = errors.New("servermanager: car class contains an empty car id")
	ErrCarClassDuplicateCar = errors.New("servermanager: car class contains a car more than once")
)

// CarClass is a named group of cars that is independent of any championship and shared by all server slots.
// Championship classes can refer to it by ID.
type CarClass struct {
	ID      uuid.UUID
	Name    string
	Cars    []string
	Created time.Time
	Updated time.Time
}

// NewCarClass creates a validated car class with a new ID. The name is trimmed.
func NewCarClass(name string, cars []string, now time.Time) (*CarClass, error) {
	class := &CarClass{
		ID:      uuid.New(),
		Name:    strings.TrimSpace(name),
		Cars:    append([]string(nil), cars...),
		Created: now,
		Updated: now,
	}

	if err := class.Validate(); err != nil {
		return nil, err
	}

	return class, nil
}

// Validate checks the rules that need nothing but the class itself: a name, at least one car, no empty and no
// duplicate car ids. Car ids are compared exactly (they are folder names); name uniqueness needs the other classes,
// see CheckCarClassNameUnique.
func (c *CarClass) Validate() error {
	if NormalizeCarClassName(c.Name) == "" {
		return ErrCarClassNameEmpty
	}

	if len(c.Cars) == 0 {
		return ErrCarClassNoCars
	}

	seen := make(map[string]bool, len(c.Cars))

	for _, car := range c.Cars {
		if strings.TrimSpace(car) == "" {
			return ErrCarClassEmptyCar
		}

		if seen[car] {
			return ErrCarClassDuplicateCar
		}

		seen[car] = true
	}

	return nil
}

// HasCar reports whether the car is part of the class.
func (c *CarClass) HasCar(car string) bool {
	for _, own := range c.Cars {
		if own == car {
			return true
		}
	}

	return false
}

// NormalizeCarClassName is the form in which names are compared for uniqueness: trimmed and case insensitive.
func NormalizeCarClassName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// CheckCarClassNameUnique returns ErrCarClassNameTaken if another class (a different ID) in classes has the same
// name as class after trimming and ignoring case.
func CheckCarClassNameUnique(class *CarClass, classes []*CarClass) error {
	name := NormalizeCarClassName(class.Name)

	for _, other := range classes {
		if other.ID != class.ID && NormalizeCarClassName(other.Name) == name {
			return ErrCarClassNameTaken
		}
	}

	return nil
}
