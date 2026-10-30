package web

// MaxDriverGUIDLength is the longest GUID accepted in a driver profile URL.
const MaxDriverGUIDLength = 64

// ValidDriverGUID tells if the string can be the GUID of a driver profile. Steam GUIDs are numeric, other ids (e.g.
// with a prefix) are accepted, too. The GUIDs of driver swaps (several GUIDs joined by a separator) have no profile.
func ValidDriverGUID(guid string) bool {
	if guid == "" || len(guid) > MaxDriverGUIDLength {
		return false
	}

	for _, c := range guid {
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && c != '_' && c != '-' {
			return false
		}
	}

	return true
}

// DriverURL returns the path of the profile of the driver with the GUID, empty if the GUID has no profile.
func DriverURL(guid string) string {
	if !ValidDriverGUID(guid) {
		return ""
	}

	return "/driver/" + guid
}
