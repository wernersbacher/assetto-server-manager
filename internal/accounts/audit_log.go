package accounts

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
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
			if url == r.URL.Path {
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

// auditPageSize is the number of entries on one page of the audit log.
const auditPageSize = 100

// AuditFilter narrows the audit log: every empty field matches everything.
type AuditFilter struct {
	// User is a case insensitive part of the user name.
	User string
	// Method is the HTTP method (case insensitive).
	Method string
	// From and To are the first and the last day (inclusive, local time) of the entries.
	From, To time.Time
}

func (f AuditFilter) IsActive() bool {
	return f.User != "" || f.Method != "" || !f.From.IsZero() || !f.To.IsZero()
}

// Matches tells whether an entry passes the filter.
func (f AuditFilter) Matches(entry *smmodel.AuditEntry) bool {
	if f.User != "" && !strings.Contains(strings.ToLower(entry.User), strings.ToLower(f.User)) {
		return false
	}

	if f.Method != "" && !strings.EqualFold(entry.Method, f.Method) {
		return false
	}

	if !f.From.IsZero() && entry.Time.Before(f.From) {
		return false
	}

	// To is a day: the entries of that day are included
	if !f.To.IsZero() && !entry.Time.Before(f.To.AddDate(0, 0, 1)) {
		return false
	}

	return true
}

// FilterAuditEntries returns the entries which pass the filter.
func FilterAuditEntries(entries []*smmodel.AuditEntry, filter AuditFilter) []*smmodel.AuditEntry {
	if !filter.IsActive() {
		return entries
	}

	var filtered []*smmodel.AuditEntry

	for _, entry := range entries {
		if filter.Matches(entry) {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// paginateAuditEntries returns the entries of a (zero based) page, the page itself (clamped to the existing pages)
// and the number of pages.
func paginateAuditEntries(entries []*smmodel.AuditEntry, page, pageSize int) ([]*smmodel.AuditEntry, int, int) {
	numPages := (len(entries) + pageSize - 1) / pageSize

	if page >= numPages {
		page = numPages - 1
	}

	if page < 0 {
		page = 0
	}

	start := page * pageSize
	end := start + pageSize

	if end > len(entries) {
		end = len(entries)
	}

	return entries[start:end], page, numPages
}

func parseAuditDay(value string) time.Time {
	day, err := time.ParseInLocation("2006-01-02", value, time.Local)

	if err != nil {
		return time.Time{}
	}

	return day
}

type auditLogTemplateVars struct {
	smweb.BaseTemplateVars

	AuditLogs   []*smmodel.AuditEntry
	Total       int
	Filter      AuditFilter
	FromValue   string
	ToValue     string
	Methods     []string
	CurrentPage int
	NumPages    int
}

func (alh *AuditLogHandler) ViewLogs(w http.ResponseWriter, r *http.Request) {
	// load server audits
	auditLogs, err := alh.store.GetAuditEntries()

	if err != nil {
		logrus.WithError(err).Error("couldn't find audit logs")
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.accounts.audit_logs_failed"))
	}

	// sort to newest first
	sort.Slice(auditLogs, func(i, j int) bool {
		return auditLogs[i].Time.After(auditLogs[j].Time)
	})

	methodSet := make(map[string]bool)

	for _, entry := range auditLogs {
		methodSet[strings.ToUpper(entry.Method)] = true
	}

	var methods []string

	for method := range methodSet {
		methods = append(methods, method)
	}

	sort.Strings(methods)

	query := r.URL.Query()
	filter := AuditFilter{
		User:   strings.TrimSpace(query.Get("user")),
		Method: strings.ToUpper(strings.TrimSpace(query.Get("method"))),
		From:   parseAuditDay(query.Get("from")),
		To:     parseAuditDay(query.Get("to")),
	}

	filtered := FilterAuditEntries(auditLogs, filter)
	pageEntries, page, numPages := paginateAuditEntries(filtered, smweb.FormValueAsInt(query.Get("page")), auditPageSize)

	var fromValue, toValue string

	if !filter.From.IsZero() {
		fromValue = filter.From.Format("2006-01-02")
	}

	if !filter.To.IsZero() {
		toValue = filter.To.Format("2006-01-02")
	}

	// render audit log page
	alh.ViewRenderer.MustLoadTemplate(w, r, "server/audit-logs.html", &auditLogTemplateVars{
		BaseTemplateVars: smweb.BaseTemplateVars{
			WideContainer: true,
		},
		AuditLogs:   pageEntries,
		Total:       len(filtered),
		Filter:      filter,
		FromValue:   fromValue,
		ToValue:     toValue,
		Methods:     methods,
		CurrentPage: page,
		NumPages:    numPages,
	})
}
