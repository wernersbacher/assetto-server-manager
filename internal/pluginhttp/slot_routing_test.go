package pluginhttp

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv/envtest"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
	"github.com/wernersbacher/assetto-server-manager/internal/web/webtest"
)

// newTestSlotFor returns a provider with a slot for each id, on a shared store: the slot "" is the default slot.
func newTestSlotFor(t *testing.T, ids ...smmodel.ServerID) (SlotFor, smstore.Store) {
	store, cleanup := envtest.TempJSONStore()
	t.Cleanup(cleanup)

	slots := make(map[smmodel.ServerID]*Slot)

	for _, id := range ids {
		serverID := id
		env := envtest.New(t.TempDir())
		env.ServerID = func() smmodel.ServerID { return serverID }

		slots[id] = &Slot{Store: smstore.ForServer(store, id), Env: env}
	}

	return func(r *http.Request) (*Slot, error) {
		slot, ok := slots[smweb.RequestedServerID(r)]

		if !ok {
			return nil, errors.New("unknown server")
		}

		return slot, nil
	}, store
}

func optionsRequest(method, target string, form url.Values, requested smmodel.ServerID) *http.Request {
	var req *http.Request

	if form != nil {
		req = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}

	req = smweb.WithScope(req, webtest.RequestScope())

	if requested != "" {
		req = smweb.WithRequestedServerID(req, requested)
	}

	return req
}

// The handlers render a template after saving, which a handler without a renderer answers with an error: the tests only
// look at what was stored.
func newTestBaseHandler() *smweb.BaseHandler {
	return smweb.NewBaseHandler(&smweb.Renderer{})
}

func TestKissMyRankOptionsUseSlotOfRequest(t *testing.T) {
	slotFor, store := newTestSlotFor(t, "", "b")
	handler := NewKissMyRankHandler(newTestBaseHandler(), slotFor, nil)

	// saving with server=b changes the options of b only

	form := url.Values{}
	form.Set("WebStatsServerPort", "7456")

	handler.Options(httptest.NewRecorder(), optionsRequest(http.MethodPost, "/kissmyrank/options", form, "b"))

	if loaded, err := store.LoadKissMyRankOptionsFor("b"); err != nil || loaded.WebStatsServerPort != 7456 {
		t.Errorf("expected the options of b to be saved, got (%v, %v)", loaded, err)
	}

	if loaded, err := store.LoadKissMyRankOptions(); err != nil || loaded.WebStatsServerPort == 7456 {
		t.Errorf("the default slot must not be changed, got (%v, %v)", loaded, err)
	}

	// an unknown server is a 404
	rec := httptest.NewRecorder()
	handler.Options(rec, optionsRequest(http.MethodGet, "/kissmyrank/options", nil, "missing"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", rec.Code)
	}
}

func TestRealPenaltyOptionsUseSlotOfRequest(t *testing.T) {
	slotFor, store := newTestSlotFor(t, "", "b")
	handler := NewRealPenaltyHandler(newTestBaseHandler(), slotFor, nil)

	if _, err := store.LoadRealPenaltyOptionsFor("b"); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("RealPenaltyAppConfig.General.AppTCPPort", "53999")

	handler.Options(httptest.NewRecorder(), optionsRequest(http.MethodPost, "/realpenalty/options", form, "b"))

	if loaded, err := store.LoadRealPenaltyOptionsFor("b"); err != nil || loaded.RealPenaltyAppConfig.General.AppTCPPort != 53999 {
		t.Errorf("expected the options of b to be saved, got (%v, %v)", loaded, err)
	}

	if loaded, err := store.LoadRealPenaltyOptions(); err != nil || loaded.RealPenaltyAppConfig.General.AppTCPPort == 53999 {
		t.Errorf("the default slot must not be changed, got (%v, %v)", loaded, err)
	}

	rec := httptest.NewRecorder()
	handler.DownloadLogs(rec, optionsRequest(http.MethodGet, "/realpenalty/logs", nil, "missing"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", rec.Code)
	}
}

func TestStrackerOptionsAndProxyUseSlotOfRequest(t *testing.T) {
	slotFor, store := newTestSlotFor(t, "", "b")
	handler := NewStrackerHandler(newTestBaseHandler(), slotFor, nil)

	if _, err := store.LoadStrackerOptionsFor("b"); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("InstanceConfiguration.ListeningPort", "50777")

	handler.Options(httptest.NewRecorder(), optionsRequest(http.MethodPost, "/stracker/options", form, "b"))

	if loaded, err := store.LoadStrackerOptionsFor("b"); err != nil || loaded.InstanceConfiguration.ListeningPort != 50777 {
		t.Errorf("expected the options of b to be saved, got (%v, %v)", loaded, err)
	}

	if loaded, err := store.LoadStrackerOptions(); err != nil || loaded.InstanceConfiguration.ListeningPort == 50777 {
		t.Errorf("the default slot must not be changed, got (%v, %v)", loaded, err)
	}

	// each slot proxies to the stracker web interface of its own options
	backends := make(map[smmodel.ServerID]string)

	for id, body := range map[smmodel.ServerID]string{"": "stracker default", "b": "stracker b"} {
		body := body

		backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		defer backend.Close()

		backends[id] = backend.Listener.Addr().String()
	}

	for id, address := range backends {
		options, err := store.LoadStrackerOptionsFor(id)

		if err != nil {
			t.Fatal(err)
		}

		host, portStr, _ := net.SplitHostPort(address)
		port, _ := strconv.Atoi(portStr)

		options.HTTPConfiguration.ListenAddress = host
		options.HTTPConfiguration.ListenPort = port

		if err := store.UpsertStrackerOptionsFor(id, options); err != nil {
			t.Fatal(err)
		}
	}

	// the proxies are created on the first request
	handler = NewStrackerHandler(newTestBaseHandler(), slotFor, nil)

	for id, want := range map[smmodel.ServerID]string{"": "stracker default", "b": "stracker b"} {
		rec := httptest.NewRecorder()
		handler.Proxy(rec, optionsRequest(http.MethodGet, "/stracker/", nil, id))

		if rec.Body.String() != want {
			t.Errorf("server %q: expected the proxy to serve %q, got %q (status %d)", id, want, rec.Body.String(), rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.Proxy(rec, optionsRequest(http.MethodGet, "/stracker/", nil, "missing"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for an unknown server, got %d", rec.Code)
	}
}
