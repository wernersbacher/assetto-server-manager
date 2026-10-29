package model

import (
	"time"
)

type AuditEntry struct {
	UserGroup Group
	Method    string
	URL       string
	User      string
	Time      time.Time
}
