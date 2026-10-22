package querylog

import (
	"bufio"
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
)

// readFiles searches the daily csv files in dir, newest day first.
func readFiles(dir string, f filter, days, limit int) ([]Entry, error) {
	out := []Entry{} // encode as [] rather than null when nothing matches
	now := time.Now()
	for i := 0; i < days && len(out) < limit; i++ {
		files, err := filesForDay(dir, now.AddDate(0, 0, -i))
		if err != nil {
			return nil, err
		}
		var day []Entry
		for _, path := range files {
			entries, err := readFile(path, f, limit)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			day = append(day, entries...)
		}
		sort.SliceStable(day, func(a, b int) bool { return day[a].Time.After(day[b].Time) })
		if n := limit - len(out); len(day) > n {
			day = day[:n]
		}
		out = append(out, day...)
	}
	return out, nil
}

// filesForDay finds log files for one day. Blocky writes YYYY-MM-DD_ALL.log
// (csv) or YYYY-MM-DD_<client>.log (csv-client); .csv variants are also accepted.
func filesForDay(dir string, d time.Time) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, d.Format("2006-01-02")+"*"))
	if err != nil {
		return nil, err
	}
	if alt, _ := filepath.Glob(filepath.Join(dir, "query-"+d.Format("2006-01-02")+"*")); len(alt) > 0 {
		matches = append(matches, alt...)
	}
	var files []string
	for _, m := range matches {
		if ext := filepath.Ext(m); ext == ".log" || ext == ".csv" {
			files = append(files, m)
		}
	}
	return files, nil
}

// blockyColumns is the column order of Blocky's headerless file log.
var blockyColumns = []string{"time", "clientip", "clientname", "duration", "reason", "question", "answer", "returncode", "responsetype", "questiontype", "instance"}

var columnAliases = map[string]string{
	"client_ip": "clientip", "client_name": "clientname", "clientnames": "clientname",
	"query": "question", "question_name": "question", "questionname": "question",
	"response_type": "responsetype",
	"return_code":   "returncode", "responsecode": "returncode", "response_code": "returncode",
	"responsereason": "reason", "response_reason": "reason",
	"responseanswer": "answer", "response_answer": "answer",
	"durationms": "duration", "duration_ms": "duration",
	"question_type": "questiontype",
	"timestamp":     "time", "date": "time", "start": "time",
}

// readFile returns at most limit matching entries from the end of the file.
func readFile(path string, f filter, limit int) ([]Entry, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	br := bufio.NewReader(fh)
	first, _ := br.Peek(4096)
	cr := csv.NewReader(br)
	if strings.Contains(string(first), "\t") {
		cr.Comma = '\t'
	}
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true

	index := map[string]int{}
	for i, name := range blockyColumns {
		index[name] = i
	}
	headerChecked := false

	var out []Entry
	for {
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !headerChecked {
			headerChecked = true
			if len(row) > 0 && parseTime(row[0]).IsZero() {
				index = headerIndex(row)
				continue
			}
		}

		e := toEntry(row, index)
		if !f.match(e) {
			continue
		}
		out = append(out, e)
		if len(out) >= 2*limit {
			out = append(out[:0], out[len(out)-limit:]...)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func headerIndex(row []string) map[string]int {
	index := map[string]int{}
	for i, h := range row {
		k := strings.ToLower(strings.TrimSpace(h))
		if alias, ok := columnAliases[k]; ok {
			k = alias
		}
		if _, seen := index[k]; !seen {
			index[k] = i
		}
	}
	return index
}

func toEntry(row []string, index map[string]int) Entry {
	get := func(name string) string {
		if i, ok := index[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	e := Entry{
		Time:         parseTime(get("time")),
		ClientIP:     get("clientip"),
		ClientName:   get("clientname"),
		Question:     get("question"),
		QuestionType: get("questiontype"),
		ResponseType: get("responsetype"),
		ResponseCode: get("returncode"),
		Reason:       get("reason"),
		Answer:       get("answer"),
	}
	e.DurationMS, _ = strconv.ParseFloat(get("duration"), 64)
	return e
}

func parseTime(v string) time.Time {
	v = strings.TrimSpace(v)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999 -0700 MST", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}
