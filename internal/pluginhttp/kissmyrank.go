package pluginhttp

import (
	"html/template"
	"net/http"

	"github.com/sirupsen/logrus"

	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

type kissMyRankConfigurationTemplateVars struct {
	smweb.BaseTemplateVars

	Form           template.HTML
	IsKMRInstalled bool
	PortWarnings   []string
}

type KissMyRankHandler struct {
	*smweb.BaseHandler
	slotFor      SlotFor
	portWarnings smweb.PortWarnings
}

func NewKissMyRankHandler(baseHandler *smweb.BaseHandler, slotFor SlotFor, portWarnings smweb.PortWarnings) *KissMyRankHandler {
	return &KissMyRankHandler{
		BaseHandler:  baseHandler,
		slotFor:      slotFor,
		portWarnings: portWarnings,
	}
}

func (kmrh *KissMyRankHandler) Options(w http.ResponseWriter, r *http.Request) {
	slot, ok := slotOrError(kmrh.slotFor, w, r)

	if !ok {
		return
	}

	opts, err := slot.Store.LoadKissMyRankOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load kissmyrank options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		err := smweb.DecodeFormData(opts, r)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't submit form")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		err = slot.Store.UpsertKissMyRankOptions(opts)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't save KissMyRank options")
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.plugins.kissmyrank_not_saved"))
		} else {
			smweb.AddFlash(w, r, smweb.T(r, "flash.plugins.kissmyrank_saved"))
		}
	}

	form, err := smweb.EncodeFormData(opts, r)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't encode form data")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	kmrh.ViewRenderer.MustLoadTemplate(w, r, "server/kissmyrank-options.html", &kissMyRankConfigurationTemplateVars{
		Form:           form,
		IsKMRInstalled: smplugins.IsKissMyRankInstalled(slot.Env.InstallPath()),
		PortWarnings:   kmrh.portWarnings.For(slot.Env.ServerID()),
	})
}
