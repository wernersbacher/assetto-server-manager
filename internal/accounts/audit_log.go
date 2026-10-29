package accounts

import (
	"net/http"
	"sort"
	"time"

	"github.com/sirupsen/logrus"

	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
	smstore "github.com/wernersbacher/assetto-server-manager/internal/store"
	smweb "github.com/wernersbacher/assetto-server-manager/internal/web"
)

var ignoredURLs = [5]string{
	"/audit-logs",
	"/quick",
	"/logs",
	"/custom",
	"/api/logs",
}

type AuditLogHandler struct {
	*smweb.BaseHandler

	store smstore.Store
}

func NewAuditLogHandler(baseHandler *smweb.BaseHandler, store smstore.Store) *AuditLogHandler {
	return &AuditLogHandler{
		BaseHandler: baseHandler,
		store:       store,
	}
}

func (alh *AuditLogHandler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, url := range ignoredURLs {
			if url == r.URL.String() {
				next.ServeHTTP(w, r)
				return
			}
		}

		account := smweb.AccountFromRequest(r)

		if account == nil {
			next.ServeHTTP(w, r)
			return
		}

		entry := &smmodel.AuditEntry{
			UserGroup: account.GroupFor(smweb.ScopeFromRequest(r).ServerID),
			Method:    r.Method,
			URL:       r.URL.String(),
			User:      account.Name,
			Time:      time.Now(),
		}

		err := alh.store.AddAuditEntry(entry)

		if err != nil {
			logrus.WithError(err).Error("Couldn't add audit entry for request")
		}

		next.ServeHTTP(w, r)
	})
}

type auditLogTemplateVars struct {
	smweb.BaseTemplateVars

	AuditLogs []*smmodel.AuditEntry
}

func (alh *AuditLogHandler) ViewLogs(w http.ResponseWriter, r *http.Request) {
	// load server audits
	auditLogs, err := alh.store.GetAuditEntries()

	if err != nil {
		logrus.WithError(err).Error("couldn't find audit logs")
		smweb.AddErrorFlash(w, r, "Couldn't open audit logs")
	}

	// sort to newest first
	sort.Slice(auditLogs, func(i, j int) bool {
		return auditLogs[i].Time.After(auditLogs[j].Time)
	})

	// render audit log page
	alh.ViewRenderer.MustLoadTemplate(w, r, "server/audit-logs.html", &auditLogTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{
			WideContainer: true,
		},
		AuditLogs: auditLogs,
	})
}
