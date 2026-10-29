package content

import (
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	"github.com/wernersbacher/assetto-server-manager/internal/results"
)

var nameRegex = regexp.MustCompile(`^[A-Za-z]{0,5}[0-9]+`)

// PrettifyName turns a content key such as "ks_ferrari_488_gt3" into a readable name. If the car name cache is
// enabled (see NewCarManager) and knows the key, the name from the car's details is used instead.
func PrettifyName(s string, acronyms bool) string {
	if s == serverconfig.AnyCarModel {
		return "Any Car Model"
	}

	if carName, ok := carNameCache.get(s); ok {
		return carName
	}

	parts := strings.Split(s, "_")

	if parts[0] == "ks" {
		parts = parts[1:]
	}

	for i := range parts {
		if (acronyms && len(parts[i]) <= 3) || (acronyms && nameRegex.MatchString(parts[i])) {
			parts[i] = strings.ToUpper(parts[i])
		} else {
			parts[i] = strings.Title(strings.ToLower(parts[i]))
		}
	}

	return strings.Join(parts, " ")
}

// CarList returns the sorted, readable names of the distinct cars in cars, separated by commas. cars can be a ';'
// separated string of car models, a slice of models, the cars of results or an entry list.
func CarList(cars interface{}) string {
	var split []string

	switch cars := cars.(type) {
	case string:
		split = strings.Split(cars, ";")
	case []string:
		split = cars
	case []*results.SessionCar:
		carMap := make(map[string]bool)

		for _, entrant := range cars {
			carMap[entrant.Model] = true
		}

		for car := range carMap {
			split = append(split, car)
		}
	case serverconfig.EntryList:
		carMap := make(map[string]bool)

		for _, entrant := range cars {
			carMap[entrant.Model] = true
		}

		for car := range carMap {
			split = append(split, car)
		}
	default:
		panic("unknown type of cars: " + reflect.TypeOf(cars).String())
	}

	var out []string

	for _, s := range split {
		if s == serverconfig.AnyCarModel {
			continue
		}

		out = append(out, PrettifyName(s, true))
	}

	sort.Strings(out)

	return strings.Join(out, ", ")
}
