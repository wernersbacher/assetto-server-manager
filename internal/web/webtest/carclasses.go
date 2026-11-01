package webtest

import (
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

// CarClasses is a web.CarClasses with fixed classes.
type CarClasses []*smmodel.CarClass

// List returns the classes.
func (c CarClasses) List() ([]*smmodel.CarClass, error) { return c, nil }

// Get returns the class with the ID, or smmodel.ErrCarClassNotFound.
func (c CarClasses) Get(id string) (*smmodel.CarClass, error) {
	for _, class := range c {
		if class.ID.String() == id {
			return class, nil
		}
	}

	return nil, smmodel.ErrCarClassNotFound
}
