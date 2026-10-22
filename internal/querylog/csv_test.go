package querylog

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/trappiz/blocky-dashboard/internal/config"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadBlockyFormatNewestFirst(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	writeFile(t, dir, yesterday+"_ALL.log",
		yesterday+" 23:00:00\t10.0.0.2\tlaptop\t3\tRESOLVED (upstream)\told.example\tA (1.2.3.4)\tNOERROR\tRESOLVED\tA\tdns1\n")
	writeFile(t, dir, today+"_ALL.log",
		today+" 00:00:01\t10.0.0.2\tlaptop\t5\tRESOLVED (upstream)\tfirst.example\tA (1.2.3.4)\tNOERROR\tRESOLVED\tA\tdns1\n"+
			today+" 00:00:02\t10.0.0.3\tphone\t0\tBLOCKED (ads)\tads.example\t\tNXDOMAIN\tBLOCKED\tA\tdns1\n"+
			today+" 00:00:03\t10.0.0.2\tlaptop\t1\tCACHED\tlast.example\tA (1.2.3.4)\tNOERROR\tCACHED\tAAAA\tdns1\n")

	r := New(config.QueryLogConfig{Type: "csv", Target: dir, Days: 2})

	got, err := r.Read(context.Background(), "", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Question != "last.example" || got[1].Question != "ads.example" {
		t.Fatalf("want newest two entries, got %+v", got)
	}
	if got[0].QuestionType != "AAAA" || got[0].ClientName != "laptop" || got[0].DurationMS != 1 {
		t.Fatalf("columns parsed wrong: %+v", got[0])
	}

	got, _ = r.Read(context.Background(), "", "", "", 10)
	if len(got) != 4 || got[3].Question != "old.example" {
		t.Fatalf("want all four entries across days, got %d", len(got))
	}

	got, _ = r.Read(context.Background(), "", "phone", "blocked", 10)
	if len(got) != 1 || got[0].Question != "ads.example" {
		t.Fatalf("client/type filter failed: %+v", got)
	}
}

func TestReadCSVWithHeader(t *testing.T) {
	dir := t.TempDir()
	today := time.Now().Format("2006-01-02")
	writeFile(t, dir, today+".csv",
		"time,client_ip,client_name,question,response_type,return_code,response_reason,duration_ms\n"+
			today+" 08:00:00,10.0.0.9,tv,cdn.example,RESOLVED,NOERROR,RESOLVED (upstream),12\n")

	got, err := New(config.QueryLogConfig{Type: "csv", Target: dir}).Read(context.Background(), "cdn", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ClientName != "tv" || got[0].DurationMS != 12 || got[0].Time.Hour() != 8 {
		t.Fatalf("header csv parsed wrong: %+v", got)
	}
}

func TestReadNoMatchesIsEmptyNotNil(t *testing.T) {
	r := New(config.QueryLogConfig{Type: "csv", Target: t.TempDir(), Days: 1})
	got, err := r.Read(context.Background(), "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice (JSON []), got %#v", got)
	}
}
