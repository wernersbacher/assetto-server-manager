package servermanager

import (
	"archive/zip"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	smplugins "github.com/wernersbacher/assetto-server-manager/internal/plugins"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

type RealPenaltyHandler struct {
	*smweb.BaseHandler

	store smstore.Store
}

func NewRealPenaltyHandler(baseHandler *smweb.BaseHandler, store smstore.Store) *RealPenaltyHandler {
	return &RealPenaltyHandler{BaseHandler: baseHandler, store: store}
}

type realPenaltyConfigurationTemplateVars struct {
	smweb.BaseTemplateVars

	Form                        template.HTML
	IsRealPenaltyInstalled      bool
	RealPenaltySupportedVersion string
}

func (rph *RealPenaltyHandler) options(w http.ResponseWriter, r *http.Request) {
	realPenaltyOptions, err := rph.store.LoadRealPenaltyOptions()

	if err != nil {
		logrus.WithError(err).Errorf("couldn't load real penalty options")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodPost {
		err := smweb.DecodeFormData(realPenaltyOptions, r)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't submit form")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		err = rph.store.UpsertRealPenaltyOptions(realPenaltyOptions)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't save Real Penalty options")
			smweb.AddErrorFlash(w, r, "Failed to save Real Penalty options")
		} else {
			smweb.AddFlash(w, r, "Real Penalty options successfully saved!")
		}
	}

	form, err := smweb.EncodeFormData(realPenaltyOptions, r)

	if err != nil {
		logrus.WithError(err).Errorf("Couldn't encode form data")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	rph.ViewRenderer.MustLoadTemplate(w, r, "server/realpenalty-options.html", &realPenaltyConfigurationTemplateVars{
		Form:                        form,
		IsRealPenaltyInstalled:      smplugins.IsRealPenaltyInstalled(ServerInstallPath),
		RealPenaltySupportedVersion: smplugins.RealPenaltySupportedVersion,
	})
}

func (rph *RealPenaltyHandler) downloadLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Disposition", fmt.Sprintf(`attachment;filename="realpenalty_logs_%s.zip"`, time.Now().Format("2006-01-02_15_04")))
	w.Header().Set("Content-Type", "application/zip")

	if err := rph.buildLogZip(w); err != nil {
		logrus.WithError(err).Errorf("Could not create real penalty log zip")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func (rph *RealPenaltyHandler) buildLogZip(w io.Writer) error {
	z := zip.NewWriter(w)
	defer z.Close()

	logFiles, err := ioutil.ReadDir(filepath.Join(smplugins.RealPenaltyFolderPath(ServerInstallPath), "logs"))

	if err != nil {
		return err
	}

	for _, file := range logFiles {
		if err := rph.writeRealPenaltyLogFileToZip(z, file); err != nil {
			return err
		}
	}

	return nil
}

func (rph *RealPenaltyHandler) writeRealPenaltyLogFileToZip(z *zip.Writer, info os.FileInfo) error {
	zf, err := z.Create(info.Name())

	if err != nil {
		return err
	}

	f, err := os.Open(filepath.Join(filepath.Join(smplugins.RealPenaltyFolderPath(ServerInstallPath), "logs"), info.Name()))

	if err != nil {
		return err
	}

	defer f.Close()

	_, err = io.Copy(zf, f)

	return err
}
