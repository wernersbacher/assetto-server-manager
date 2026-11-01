package pluginhttp

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/net/html"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
	"github.com/wernersbacher/ACPitwall/internal/portcheck"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

type StrackerHandler struct {
	*smweb.BaseHandler

	slotFor      SlotFor
	portWarnings smweb.PortWarnings

	// reverseProxies are the proxies to the stracker web interface of each slot, by the id of the slot
	reverseProxies map[smmodel.ServerID]*httputil.ReverseProxy
	mutex          sync.Mutex
}

func NewStrackerHandler(baseHandler *smweb.BaseHandler, slotFor SlotFor, portWarnings smweb.PortWarnings) *StrackerHandler {
	return &StrackerHandler{BaseHandler: baseHandler, slotFor: slotFor, portWarnings: portWarnings, reverseProxies: make(map[smmodel.ServerID]*httputil.ReverseProxy)}
}

type strackerConfigurationTemplateVars struct {
	smweb.BaseTemplateVars

	Form                template.HTML
	IsStrackerInstalled bool
	PortWarnings        []portcheck.Warning
}

// initReverseProxy (re-)creates the proxy to the stracker web interface of the slot, with the address from its options.
func (sth *StrackerHandler) initReverseProxy(slot *Slot) error {
	strackerOptions, err := slot.Store.LoadStrackerOptions()

	if err != nil {
		return err
	}

	proxyURL, err := url.Parse(fmt.Sprintf("http://%s:%d/", strackerOptions.HTTPConfiguration.ListenAddress, strackerOptions.HTTPConfiguration.ListenPort))

	if err != nil {
		return err
	}

	reverseProxy := httputil.NewSingleHostReverseProxy(proxyURL)
	reverseProxy.Director = func(req *http.Request) {
		req.Header.Add("X-Forwarded-Host", req.Host)
		req.Header.Add("X-Origin-Host", proxyURL.Host)
		req.Header.Add("Accept-Encoding", "identity")
		req.URL.Scheme = "http"
		req.URL.Host = proxyURL.Host
	}

	reverseProxy.Transport = &http.Transport{DisableCompression: true}
	reverseProxy.FlushInterval = time.Millisecond * 200

	reverseProxy.ModifyResponse = func(r *http.Response) error {
		if r.Header.Get("Content-Type") != "text/html;charset=utf-8" {
			// only attempt to modify html responses
			return nil
		}

		z, err := html.Parse(r.Body)

		if err != nil {
			return err
		}

		recurseStrackerProxyHTMLTree(z)

		buf := new(bytes.Buffer)

		err = html.Render(buf, z)

		if err != nil {
			return err
		}

		if err := r.Body.Close(); err != nil {
			return err
		}

		r.Body = ioutil.NopCloser(buf)
		r.Header.Set("Content-Length", fmt.Sprint(buf.Len()))

		return nil
	}

	sth.mutex.Lock()
	sth.reverseProxies[slot.Env.ServerID()] = reverseProxy
	sth.mutex.Unlock()

	return nil
}

// Proxy serves the stracker web interface of the server the request is for (the one selected by the server parameter or
// remembered in the session, as the links in the proxied pages do not carry the parameter).
func (sth *StrackerHandler) Proxy(w http.ResponseWriter, r *http.Request) {
	slot, ok := slotOrError(sth.slotFor, w, r)

	if !ok {
		return
	}

	sth.mutex.Lock()
	reverseProxy := sth.reverseProxies[slot.Env.ServerID()]
	sth.mutex.Unlock()

	if reverseProxy == nil {
		err := sth.initReverseProxy(slot)

		if err != nil {
			logrus.WithError(err).Error("Could not initialise stracker reverse proxy")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		sth.mutex.Lock()
		reverseProxy = sth.reverseProxies[slot.Env.ServerID()]
		sth.mutex.Unlock()
	}

	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/stracker")

	if r.URL.Path == "/" {
		r.URL.Path = "/mainpage"
	}

	if r.URL.Path == "/admin" {
		r.URL.Path = "/admin/mainpage"
	}

	reverseProxy.ServeHTTP(w, r)
}

// strackerLinkTagReplacements is a map of html tags to their attributes which need their links prefixing
var strackerLinkTagReplacements = map[string]map[string]bool{
	"script": {
		"src": true,
	},
	"img": {
		"src": true,
	},
	"link": {
		"href": true,
	},
	"a": {
		"href": true,
	},
	"form": {
		"action": true,
	},
}

func recurseStrackerProxyHTMLTree(n *html.Node) {
	if tag, tagIsReplaceable := strackerLinkTagReplacements[n.Data]; tagIsReplaceable {
		for attrIndex, attr := range n.Attr {
			if _, hasAttrReplacement := tag[attr.Key]; hasAttrReplacement {
				processSTrackerLink(&n.Attr[attrIndex])
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		recurseStrackerProxyHTMLTree(c)
	}
}

func processSTrackerLink(attr *html.Attribute) {
	val, err := url.Parse(attr.Val)

	if err != nil {
		// skip this resource
		return
	}

	if val.Host != "" {
		return
	}

	if strings.HasPrefix(attr.Val, "/") {
		attr.Val = "/stracker" + attr.Val
	}
}

func (sth *StrackerHandler) Options(w http.ResponseWriter, r *http.Request) {
	slot, ok := slotOrError(sth.slotFor, w, r)

	if !ok {
		return
	}

	strackerOptions, err := slot.Store.LoadStrackerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load stracker options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		err := smweb.DecodeFormData(strackerOptions, r)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't submit form")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		err = slot.Store.UpsertStrackerOptions(strackerOptions)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't save stracker options")
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.plugins.stracker_not_saved"))
		} else {
			smweb.AddFlash(w, r, smweb.T(r, "flash.plugins.stracker_saved"))
		}

		err = sth.initReverseProxy(slot)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't re-init stracker proxy")
		}
	}

	form, err := smweb.EncodeFormData(strackerOptions, r)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't encode form data")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sth.ViewRenderer.MustLoadTemplate(w, r, "server/stracker-options.html", &strackerConfigurationTemplateVars{
		Form:                form,
		IsStrackerInstalled: smplugins.IsStrackerInstalled(slot.Env.InstallPath()),
		PortWarnings:        sth.portWarnings.For(slot.Env.ServerID()),
	})
}
