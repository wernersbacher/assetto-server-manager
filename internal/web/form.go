package web

import "strconv"

// FormValueAsInt parses a form value as an integer. Checkboxes ("on") count as 1, anything unparsable as 0.
func FormValueAsInt(val string) int {
	if val == "on" {
		return 1
	}

	i, err := strconv.ParseInt(val, 10, 0)

	if err != nil {
		return 0
	}

	return int(i)
}

// FormValueAsFloat parses a form value as a float, anything unparsable counts as 0.
func FormValueAsFloat(val string) float64 {
	i, err := strconv.ParseFloat(val, 0)

	if err != nil {
		return 0
	}

	return i
}
