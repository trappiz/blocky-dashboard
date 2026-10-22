package querylog

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
)

type dialect int

const (
	dialectPostgres dialect = iota
	dialectMySQL
)

// openDB opens Blocky's query log database with the same connection string
// (DSN) Blocky uses as queryLog.target. Nothing connects until the first query.
func openDB(typ, dsn string) (*sql.DB, dialect, error) {
	var db *sql.DB
	var d dialect
	switch strings.ToLower(typ) {
	case "mysql":
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, 0, fmt.Errorf("query log target: %w", err)
		}
		cfg.ParseTime = true // scan request_ts into time.Time even if the DSN omits parseTime
		conn, err := mysql.NewConnector(cfg)
		if err != nil {
			return nil, 0, fmt.Errorf("query log target: %w", err)
		}
		db, d = sql.OpenDB(conn), dialectMySQL
	default: // postgresql, postgres, timescale
		var err error
		if db, err = sql.Open("pgx", dsn); err != nil {
			return nil, 0, fmt.Errorf("query log target: %w", err)
		}
		d = dialectPostgres
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, d, nil
}

// buildQuery selects from Blocky's log_entries table. The filters mirror
// filter.match: needle is a case-insensitive substring of the domain, client
// name or IP; client and rtype match whole values. hostname is an optional
// SQL LIKE pattern on the Blocky host that wrote the entry.
func buildQuery(d dialect, f filter, hostname string, since time.Time, limit int) (string, []any) {
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		if d == dialectPostgres {
			return "$" + strconv.Itoa(len(args))
		}
		return "?"
	}
	where := []string{"request_ts >= " + arg(since)}
	if f.needle != "" {
		like := "%" + escapeLike(f.needle) + "%"
		var or []string
		for _, col := range []string{"LOWER(question_name)", "LOWER(client_name)", "client_ip"} {
			or = append(or, col+" LIKE "+arg(like)+" ESCAPE '!'")
		}
		where = append(where, "("+strings.Join(or, " OR ")+")")
	}
	if f.client != "" {
		where = append(where, "(LOWER(client_name) = "+arg(strings.ToLower(f.client))+" OR client_ip = "+arg(f.client)+")")
	}
	if f.rtype != "" {
		where = append(where, "UPPER(response_type) = "+arg(strings.ToUpper(f.rtype)))
	}
	if hostname != "" {
		where = append(where, "hostname LIKE "+arg(hostname))
	}
	q := "SELECT request_ts, client_ip, client_name, duration_ms, reason, response_type, question_type, question_name, answer, response_code" +
		" FROM log_entries WHERE " + strings.Join(where, " AND ") +
		" ORDER BY request_ts DESC LIMIT " + strconv.Itoa(limit)
	return q, args
}

// escapeLike makes s match literally in a LIKE pattern with ESCAPE '!'.
func escapeLike(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}

func (r *Reader) readDB(ctx context.Context, f filter, since time.Time, limit int) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	q, args := buildQuery(r.dialect, f, r.cfg.Hostname, since, limit)
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query log database: %w", err)
	}
	defer rows.Close()

	out := []Entry{} // encode as [] rather than null when nothing matches
	for rows.Next() {
		var e Entry
		var ip, name, reason, rtype, qtype, question, answer, code sql.NullString
		var duration sql.NullInt64
		if err := rows.Scan(&e.Time, &ip, &name, &duration, &reason, &rtype, &qtype, &question, &answer, &code); err != nil {
			return nil, fmt.Errorf("query log database: %w", err)
		}
		e.ClientIP, e.ClientName, e.Reason, e.ResponseType = ip.String, name.String, reason.String, rtype.String
		e.QuestionType, e.Question, e.Answer, e.ResponseCode = qtype.String, question.String, answer.String, code.String
		e.DurationMS = float64(duration.Int64)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query log database: %w", err)
	}
	return out, nil
}
