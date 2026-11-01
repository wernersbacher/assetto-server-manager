package pluginhttp

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

	smplugins "github.com/wernersbacher/ACPitwall/internal/plugins"
	"github.com/wernersbacher/ACPitwall/internal/portcheck"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

type RealPenaltyHandler struct {
	*smweb.BaseHandler

	slotFor      SlotFor
	portWarnings smweb.PortWarnings
}

func NewRealPenaltyHandler(baseHandler *smweb.BaseHandler, slotFor SlotFor, portWarnings smweb.PortWarnings) *RealPenaltyHandler {
	return &RealPenaltyHandler{BaseHandler: baseHandler, slotFor: slotFor, portWarnings: portWarnings}
}

type realPenaltyConfigurationTemplateVars struct {
	smweb.BaseTemplateVars

	Form                        template.HTML
	IsRealPenaltyInstalled      bool
	RealPenaltySupportedVersion string
	PortWarnings                []portcheck.Warning
}

func (rph *RealPenaltyHandler) Options(w http.ResponseWriter, r *http.Request) {
	slot, ok := slotOrError(rph.slotFor, w, r)

	if !ok {
		return
	}

	realPenaltyOptions, err := slot.Store.LoadRealPenaltyOptions()

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

		err = slot.Store.UpsertRealPenaltyOptions(realPenaltyOptions)

		if err != nil {
			logrus.WithError(err).Errorf("couldn't save Real Penalty options")
			smweb.AddErrorFlash(w, r, smweb.T(r, "flash.plugins.realpenalty_not_saved"))
		} else {
			smweb.AddFlash(w, r, smweb.T(r, "flash.plugins.realpenalty_saved"))
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
		IsRealPenaltyInstalled:      smplugins.IsRealPenaltyInstalled(slot.Env.InstallPath()),
		RealPenaltySupportedVersion: smplugins.RealPenaltySupportedVersion,
		PortWarnings:                rph.portWarnings.For(slot.Env.ServerID()),
	})
}

func (rph *RealPenaltyHandler) DownloadLogs(w http.ResponseWriter, r *http.Request) {
	slot, ok := slotOrError(rph.slotFor, w, r)

	if !ok {
		return
	}

	w.Header().Add("Content-Disposition", fmt.Sprintf(`attachment;filename="realpenalty_logs_%s.zip"`, time.Now().Format("2006-01-02_15_04")))
	w.Header().Set("Content-Type", "application/zip")

	if err := buildLogZip(w, slot.Env.InstallPath()); err != nil {
		logrus.WithError(err).Errorf("Could not create real penalty log zip")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func buildLogZip(w io.Writer, installPath string) error {
	z := zip.NewWriter(w)
	defer z.Close()

	logFiles, err := ioutil.ReadDir(filepath.Join(smplugins.RealPenaltyFolderPath(installPath), "logs"))

	if err != nil {
		return err
	}

	for _, file := range logFiles {
		if err := writeRealPenaltyLogFileToZip(z, installPath, file); err != nil {
			return err
		}
	}

	return nil
}

func writeRealPenaltyLogFileToZip(z *zip.Writer, installPath string, info os.FileInfo) error {
	zf, err := z.Create(info.Name())

	if err != nil {
		return err
	}

	f, err := os.Open(filepath.Join(filepath.Join(smplugins.RealPenaltyFolderPath(installPath), "logs"), info.Name()))

	if err != nil {
		return err
	}

	defer f.Close()

	_, err = io.Copy(zf, f)

	return err
}
