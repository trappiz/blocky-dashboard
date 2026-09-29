package querylog

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

type Entry struct {
	Time         time.Time `json:"time"`
	ClientIP     string    `json:"clientIP"`
	ClientName   string    `json:"clientName"`
	Question     string    `json:"question"`
	ResponseType string    `json:"responseType"`
	ResponseCode string    `json:"returnCode"`
	Reason       string    `json:"reason"`
	Answer       string    `json:"answer"`
	DurationMS   float64   `json:"durationMs"`
}

type Reader struct {
	cfg config.QueryLogConfig
}

func New(cfg config.QueryLogConfig) *Reader { return &Reader{cfg: cfg} }

func (r *Reader) Enabled() bool {
	return strings.EqualFold(r.cfg.Type, "csv") && r.cfg.Target != ""
}

func (r *Reader) Read(filter, client, rtype string, limit int) ([]Entry, error) {
	if !r.Enabled() {
		return nil, errors.New("query logging is not configured")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	days := r.cfg.Days
	if days < 1 {
		days = 1
	}
	var files []string
	now := time.Now()
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -i)
		// Blocky uses YYYY-MM-DD.csv for daily CSV logs. Accept common variants.
		candidates := []string{
			filepath.Join(r.cfg.Target, d.Format("2006-01-02")+".csv"),
			filepath.Join(r.cfg.Target, "query-"+d.Format("2006-01-02")+".csv"),
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				files = append(files, p)
			}
		}
	}
	sort.Strings(files)

	var out []Entry
	for _, path := range files {
		entries, err := readFile(path, filter, client, rtype, limit-len(out))
		if err != nil {
			return out, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, entries...)
		if len(out) >= limit {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

func readFile(path, filter, client, rtype string, limit int) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cr := csv.NewReader(f)
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	for i, h := range header {
		index[strings.ToLower(strings.TrimSpace(h))] = i
	}

	get := func(row []string, names ...string) string {
		for _, n := range names {
			if i, ok := index[strings.ToLower(n)]; ok && i < len(row) {
				return row[i]
			}
		}
		return ""
	}

	var out []Entry
	for {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		e := Entry{
			ClientIP:     get(row, "clientIP", "clientip", "client_ip"),
			ClientName:   get(row, "clientName", "clientname", "client_name"),
			Question:     get(row, "question", "query"),
			ResponseType: get(row, "responseType", "response_type"),
			ResponseCode: get(row, "returnCode", "return_code", "responseCode", "response_code"),
			Reason:       get(row, "responseReason", "response_reason", "reason"),
			Answer:       get(row, "responseAnswer", "response_answer", "answer"),
		}
		if v := get(row, "duration"); v != "" {
			e.DurationMS, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
		}
		if v := get(row, "time", "timestamp", "date"); v != "" {
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05.000Z07:00"} {
				if t, x := time.Parse(layout, strings.TrimSpace(v)); x == nil {
					e.Time = t
					break
				}
			}
		}
		if e.Time.IsZero() {
			e.Time = time.Now()
		}

		needle := strings.ToLower(strings.TrimSpace(filter))
		if needle != "" && !strings.Contains(strings.ToLower(e.Question+" "+e.ClientName+" "+e.ClientIP), needle) {
			continue
		}
		if client != "" && !strings.EqualFold(client, e.ClientName) && !strings.EqualFold(client, e.ClientIP) {
			continue
		}
		if rtype != "" && !strings.EqualFold(rtype, e.ResponseType) {
			continue
		}

		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
