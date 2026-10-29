package servermanager

import (
	"html/template"
	"net/http"

	"github.com/sirupsen/logrus"

	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
)

type kissMyRankConfigurationTemplateVars struct {
	BaseTemplateVars

	Form           template.HTML
	IsKMRInstalled bool
}

type KissMyRankHandler struct {
	*BaseHandler
	store Store
}

func NewKissMyRankHandler(baseHandler *BaseHandler, store Store) *KissMyRankHandler {
	return &KissMyRankHandler{
		BaseHandler: baseHandler,
		store:       store,
	}
}

func (kmrh *KissMyRankHandler) options(w http.ResponseWriter, r *http.Request) {
	opts, err := kmrh.store.LoadKissMyRankOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load kissmyrank options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		err := DecodeFormData(opts, r)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't submit form")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		err = kmrh.store.UpsertKissMyRankOptions(opts)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't save KissMyRank options")
			AddErrorFlash(w, r, "Failed to save KissMyRank options")
		} else {
			AddFlash(w, r, "KissMyRank options successfully saved!")
		}
	}

	form, err := EncodeFormData(opts, r)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't encode form data")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	kmrh.viewRenderer.MustLoadTemplate(w, r, "server/kissmyrank-options.html", &kissMyRankConfigurationTemplateVars{
		Form:           form,
		IsKMRInstalled: smplugins.IsKissMyRankInstalled(ServerInstallPath),
	})
}
