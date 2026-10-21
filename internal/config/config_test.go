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

func TestApplyEnv(t *testing.T) {
	env := map[string]string{
		EnvAuthPassword: "s3cret",
		EnvBlockyToken:  "single",
		"BLOCKY_DASHBOARD_INSTANCE_HOME_DNS_TOKEN": "home",
	}
	c := Config{
		Auth:   AuthConfig{Password: "from-file", HeaderToken: "keep"},
		Blocky: BlockyConfig{Token: "old"},
		Instances: []InstanceConfig{
			{Name: "Home DNS", BlockyConfig: BlockyConfig{URL: "http://1"}},
			{Name: "office", BlockyConfig: BlockyConfig{URL: "http://2", Token: "file-token"}},
		},
	}
	c.ApplyEnv(func(k string) string { return env[k] })
	if c.Auth.Password != "s3cret" || c.Auth.HeaderToken != "keep" || c.Blocky.Token != "single" {
		t.Fatalf("top-level overrides wrong: %+v %+v", c.Auth, c.Blocky)
	}
	if c.Instances[0].Token != "home" || c.Instances[1].Token != "file-token" {
		t.Fatalf("instance tokens wrong: %+v", c.Instances)
	}
}

func TestInstanceTokenEnv(t *testing.T) {
	if got := InstanceTokenEnv("pi.hole #2"); got != "BLOCKY_DASHBOARD_INSTANCE_PI_HOLE_2_TOKEN" {
		t.Fatalf("got %q", got)
	}
}
