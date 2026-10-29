package pluginhttp

import (
	"html/template"
	"net/http"

	"github.com/sirupsen/logrus"

	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
	"github.com/wernersbacher/assetto-server-manager/internal/serverenv"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

type kissMyRankConfigurationTemplateVars struct {
	smweb.BaseTemplateVars

	Form           template.HTML
	IsKMRInstalled bool
}

type KissMyRankHandler struct {
	*smweb.BaseHandler
	store smstore.Store
	env   *serverenv.Env
}

func NewKissMyRankHandler(baseHandler *smweb.BaseHandler, store smstore.Store, env *serverenv.Env) *KissMyRankHandler {
	return &KissMyRankHandler{
		BaseHandler: baseHandler,
		store:       store,
		env:         env,
	}
}

func (kmrh *KissMyRankHandler) Options(w http.ResponseWriter, r *http.Request) {
	opts, err := kmrh.store.LoadKissMyRankOptions()

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

		err = kmrh.store.UpsertKissMyRankOptions(opts)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't save KissMyRank options")
			smweb.AddErrorFlash(w, r, "Failed to save KissMyRank options")
		} else {
			smweb.AddFlash(w, r, "KissMyRank options successfully saved!")
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
		IsKMRInstalled: smplugins.IsKissMyRankInstalled(kmrh.env.InstallPath()),
	})
}
