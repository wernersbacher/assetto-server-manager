package resultshttp

import (
	"net/http"
	"path/filepath"

	smresults "github.com/wernersbacher/assetto-server-manager/internal/results"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

// ServerDir is a server slot together with its results directory.
type ServerDir struct {
	smweb.ServerRef

	ResultsDir string
}

// ServersFunc returns all server slots with their results directories. The resolver provides it, as the handlers
// cannot know the slots. Results are attributed to a server by the directory they are found in, which keeps the
// results format unchanged.
type ServersFunc func() []ServerDir

// serverIndex maps the results directories to their servers.
type serverIndex struct {
	servers []ServerDir
}

// serverIndex returns the servers for attributing results. It is empty (nothing is attributed) if there are fewer than
// two servers, so that a single server setup looks and answers as before.
func (rh *ResultsHandler) serverIndex() serverIndex {
	if rh.servers == nil {
		return serverIndex{}
	}

	servers := rh.servers()

	if len(servers) < 2 {
		return serverIndex{}
	}

	return serverIndex{servers: servers}
}

// of returns the server which holds the results file, nil if there is none or the server is not attributed.
func (i serverIndex) of(loader *smresults.Loader, fileName string) *smweb.ServerRef {
	if len(i.servers) == 0 {
		return nil
	}

	dir := filepath.Clean(loader.DirOf(fileName))

	for _, server := range i.servers {
		if filepath.Clean(server.ResultsDir) == dir {
			ref := server.ServerRef

			return &ref
		}
	}

	return nil
}

// dirOf returns the results directory of the server with the given id, and false if there is no such server.
func (i serverIndex) dirOf(id string) (string, bool) {
	for _, server := range i.servers {
		if string(server.ID) == id {
			return server.ResultsDir, true
		}
	}

	return "", false
}

// filterServerParam is the query parameter which restricts the results list and API to one server. It is not the
// global server parameter, which selects the server a page is shown for.
const filterServerParam = "filterServer"

// filterServer reads filterServer from the request and sets the directory filter. An unknown server id matches
// nothing. It returns the server id as given.
func (i serverIndex) filterServer(r *http.Request, filter *smresults.Filter) string {
	id := r.URL.Query().Get(filterServerParam)

	if id == "" || len(i.servers) == 0 {
		return ""
	}

	dir, ok := i.dirOf(id)

	if !ok {
		// no results directory is named like this, so nothing matches
		dir = filepath.Join(string(filepath.Separator), "unknown-server", id)
	}

	filter.Dir = dir

	return id
}
