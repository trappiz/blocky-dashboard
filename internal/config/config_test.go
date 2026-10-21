package config

import "testing"

func TestResolvedInstancesFallsBackToSingleBlocky(t *testing.T) {
	c := Config{Blocky: BlockyConfig{URL: "http://a"}, UI: UIConfig{Instance: "home-dns"}, QueryLog: QueryLogConfig{Type: "csv"}}
	got := c.ResolvedInstances()
	if len(got) != 1 || got[0].Name != "home-dns" || got[0].URL != "http://a" || got[0].QueryLog.Type != "csv" {
		t.Fatalf("unexpected fallback: %+v", got)
	}

	c.Instances = []InstanceConfig{{Name: "one", BlockyConfig: BlockyConfig{URL: "http://1"}}, {Name: "two", BlockyConfig: BlockyConfig{URL: "http://2"}}}
	if got := c.ResolvedInstances(); len(got) != 2 || got[1].URL != "http://2" {
		t.Fatalf("instances not used: %+v", got)
	}
}

func TestValidateInstances(t *testing.T) {
	for name, in := range map[string][]InstanceConfig{
		"missing name": {{BlockyConfig: BlockyConfig{URL: "http://1"}}},
		"missing url":  {{Name: "one"}},
		"same slug":    {{Name: "DNS 1", BlockyConfig: BlockyConfig{URL: "http://1"}}, {Name: "dns-1", BlockyConfig: BlockyConfig{URL: "http://2"}}},
	} {
		if err := (Config{Instances: in}).Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	ok := []InstanceConfig{{Name: "DNS 1", BlockyConfig: BlockyConfig{URL: "http://1"}}, {Name: "DNS 2", BlockyConfig: BlockyConfig{URL: "http://2"}}}
	if err := (Config{Instances: ok}).Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{"DNS One": "dns-one", "  pi.hole #2 ": "pi-hole-2", "!!!": "instance"} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}
