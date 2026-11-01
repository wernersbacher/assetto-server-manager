package api

import (
	"github.com/google/uuid"

	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// The car classes API exposes the global car classes (named groups of cars, shared by all servers) read-only.

// CarClassCar is a car of a car class.
type CarClassCar struct {
	// ID is the model (folder name) of the car.
	ID   string
	Name string
}

// CarClass is a global car class with its cars.
type CarClass struct {
	ID   uuid.UUID
	Name string
	Cars []*CarClassCar
}

// NewCarClass returns the API representation of a car class. The cars are sorted as stored.
func NewCarClass(class *smmodel.CarClass) *CarClass {
	out := &CarClass{ID: class.ID, Name: class.Name, Cars: make([]*CarClassCar, 0, len(class.Cars))}

	for _, car := range class.Cars {
		out.Cars = append(out.Cars, &CarClassCar{ID: car, Name: smcontent.PrettifyName(car, true)})
	}

	return out
}
