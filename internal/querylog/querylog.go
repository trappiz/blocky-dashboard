// Package querylog searches Blocky's query log, stored as csv files
// (queryLog.type csv or csv-client), in a MySQL or PostgreSQL database, or
// behind a small JSON log server on the Blocky host (type http).
package querylog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

type Entry struct {
	Time         time.Time `json:"time"`
	ClientIP     string    `json:"clientIP"`
	ClientName   string    `json:"clientName"`
	Question     string    `json:"question"`
	QuestionType string    `json:"questionType"`
	ResponseType string    `json:"responseType"`
	ResponseCode string    `json:"returnCode"`
	Reason       string    `json:"reason"`
	Answer       string    `json:"answer"`
	DurationMS   float64   `json:"durationMs"`
}

// Reader searches one instance's query log.
type Reader struct {
	cfg     config.QueryLogConfig
	db      *sql.DB // nil for csv files
	dialect dialect
	http    *httpSource // set for type http
	err     error       // why the database or log server is unusable
}

// New prepares a reader. Database connections are opened on first use.
func New(cfg config.QueryLogConfig) *Reader {
	r := &Reader{cfg: cfg}
	switch {
	case !r.Enabled():
	case config.IsDatabaseQueryLog(cfg.Type):
		r.db, r.dialect, r.err = openDB(cfg.Type, cfg.Target)
	case config.IsHTTPQueryLog(cfg.Type):
		r.http, r.err = newHTTPSource(cfg.Target)
	}
	return r
}

// Enabled reports whether the query log is switched on and fully configured;
// the UI hides the query log for instances where it is not.
func (r *Reader) Enabled() bool {
	return r.cfg.IsEnabled() && config.KnownQueryLogType(r.cfg.Type) && r.cfg.Target != ""
}

type filter struct {
	needle, client, rtype string
}

func (f filter) match(e Entry) bool {
	if f.needle != "" && !strings.Contains(strings.ToLower(e.Question+" "+e.ClientName+" "+e.ClientIP), f.needle) {
		return false
	}
	if f.client != "" && !strings.EqualFold(f.client, e.ClientName) && !strings.EqualFold(f.client, e.ClientIP) {
		return false
	}
	if f.rtype != "" && !strings.EqualFold(f.rtype, e.ResponseType) {
		return false
	}
	return true
}

// Read returns the newest entries matching the filters, newest first.
func (r *Reader) Read(ctx context.Context, needle, client, rtype string, limit int) ([]Entry, error) {
	if !r.Enabled() {
		return nil, errors.New("query logging is not configured")
	}
	if r.err != nil {
		return nil, r.err
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	days := r.cfg.Days
	if days < 1 {
		days = 1
	}
	f := filter{needle: strings.ToLower(strings.TrimSpace(needle)), client: strings.TrimSpace(client), rtype: strings.TrimSpace(rtype)}
	if r.http != nil {
		return r.http.read(ctx, f, limit)
	}
	if r.db != nil {
		y, m, d := time.Now().AddDate(0, 0, 1-days).Date()
		return r.readDB(ctx, f, time.Date(y, m, d, 0, 0, 0, 0, time.Local), limit)
	}
	return readFiles(r.cfg.Target, f, days, limit)
}

// Close releases the database connections, if any.
func (r *Reader) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}
