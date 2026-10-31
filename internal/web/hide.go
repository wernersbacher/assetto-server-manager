package web

import (
	"net/http"

	smresults "github.com/wernersbacher/ACPitwall/internal/results"
)

// HideFuncFor returns the filter for results of hidden championships and race weekends that applies to a request: nil
// if the requester may see everything or nothing is hidden. Handler packages that cannot import the package which
// knows the hidden entries are given one by the resolver; a nil HideFuncFor hides nothing.
type HideFuncFor func(r *http.Request) smresults.HideFunc

// For returns the filter for the request.
func (h HideFuncFor) For(r *http.Request) smresults.HideFunc {
	if h == nil {
		return nil
	}

	return h(r)
}
