package content

import (
	"regexp"
	"strings"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
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
