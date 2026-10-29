package servermanager

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/net/html"

	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

type StrackerHandler struct {
	*BaseHandler

	store        smstore.Store
	reverseProxy *httputil.ReverseProxy
}

func NewStrackerHandler(baseHandler *BaseHandler, store smstore.Store) *StrackerHandler {
	return &StrackerHandler{BaseHandler: baseHandler, store: store}
}

type strackerConfigurationTemplateVars struct {
	BaseTemplateVars

	Form                template.HTML
	IsStrackerInstalled bool
}

func (sth *StrackerHandler) initReverseProxy() error {
	strackerOptions, err := sth.store.LoadStrackerOptions()

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

	sth.reverseProxy = reverseProxy

	return nil
}

func (sth *StrackerHandler) proxy(w http.ResponseWriter, r *http.Request) {
	if sth.reverseProxy == nil {
		err := sth.initReverseProxy()

		if err != nil {
			logrus.WithError(err).Error("Could not initialise stracker reverse proxy")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/stracker")

	if r.URL.Path == "/" {
		r.URL.Path = "/mainpage"
	}

	if r.URL.Path == "/admin" {
		r.URL.Path = "/admin/mainpage"
	}

	sth.reverseProxy.ServeHTTP(w, r)
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

func (sth *StrackerHandler) options(w http.ResponseWriter, r *http.Request) {
	strackerOptions, err := sth.store.LoadStrackerOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load stracker options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		err := DecodeFormData(strackerOptions, r)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't submit form")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		err = sth.store.UpsertStrackerOptions(strackerOptions)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't save stracker options")
			smweb.AddErrorFlash(w, r, "Failed to save stracker options")
		} else {
			smweb.AddFlash(w, r, "Stracker options successfully saved!")
		}

		err = sth.initReverseProxy()

		if err != nil {
			logrus.WithError(err).Errorf("couldn't re-init stracker proxy")
		}
	}

	form, err := EncodeFormData(strackerOptions, r)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't encode form data")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	sth.viewRenderer.MustLoadTemplate(w, r, "server/stracker-options.html", &strackerConfigurationTemplateVars{
		Form:                form,
		IsStrackerInstalled: smplugins.IsStrackerInstalled(ServerInstallPath),
	})
}
