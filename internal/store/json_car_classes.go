package store

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

const carClassesDir = "car-classes"

// canonicalCarClassID accepts UUIDs only, because the JSON store builds file names from ids.
func canonicalCarClassID(id string) (string, error) {
	parsed, err := uuid.Parse(id)

	if err != nil {
		return "", smmodel.ErrCarClassNotFound
	}

	return parsed.String(), nil
}

// sortCarClasses orders classes by name (case insensitive) and id, so both stores list them identically.
func sortCarClasses(classes []*smmodel.CarClass) {
	sort.SliceStable(classes, func(i, j int) bool {
		a, b := strings.ToLower(classes[i].Name), strings.ToLower(classes[j].Name)

		if a != b {
			return a < b
		}

		return classes[i].ID.String() < classes[j].ID.String()
	})
}

func (rs *JSONStore) UpsertCarClass(class *smmodel.CarClass) error {
	class.Updated = time.Now()

	return rs.encodeFile(rs.shared, filepath.Join(carClassesDir, class.ID.String()+".json"), class)
}

func (rs *JSONStore) FindCarClassByID(id string) (*smmodel.CarClass, error) {
	id, err := canonicalCarClassID(id)

	if err != nil {
		return nil, err
	}

	var class *smmodel.CarClass

	err = rs.decodeFile(rs.shared, filepath.Join(carClassesDir, id+".json"), &class)

	if os.IsNotExist(err) {
		return nil, smmodel.ErrCarClassNotFound
	} else if err != nil {
		return nil, err
	}

	return class, nil
}

func (rs *JSONStore) ListCarClasses() ([]*smmodel.CarClass, error) {
	files, err := rs.listFiles(filepath.Join(rs.shared, carClassesDir))

	if err != nil {
		return nil, err
	}

	var classes []*smmodel.CarClass

	for _, file := range files {
		if class, err := rs.FindCarClassByID(file); err == nil {
			classes = append(classes, class)
		}
	}

	sortCarClasses(classes)

	return classes, nil
}

func (rs *JSONStore) DeleteCarClass(id string) error {
	if _, err := rs.FindCarClassByID(id); err != nil {
		return err
	}

	id, _ = canonicalCarClassID(id)

	return os.Remove(filepath.Join(rs.shared, carClassesDir, id+".json"))
}
