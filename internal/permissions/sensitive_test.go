package permissions

import "testing"

func TestIsSensitivePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{".env.production", true},
		{"config/.env", true},
		{"secrets/app.pem", true},
		{"app.key", true},
		{"cert.p12", true},
		{"cert.pfx", true},
		{".ssh/id_rsa", true},
		{"home/user/.ssh/config", true},
		{"id_ed25519", true},
		{"authorized_keys", true},
		{"known_hosts", true},
		{".aws/credentials", true},
		{".kube/config", true},
		{".docker/config.json", true},
		{".netrc", true},
		{".git-credentials", true},
		{".npmrc", true},
		{".pypirc", true},
		{"src/main.go", false},
		{"README.md", false},
		{"environment.txt", false}, // must not match substring "env"
		{"docs/dev-environment", false},
		{".gitignore", false},
		{"package.json", false},
		{"", false},
		{"   ", false},
	}
	for _, c := range cases {
		if got := IsSensitivePath(c.path); got != c.want {
			t.Errorf("IsSensitivePath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestSensitivePathEscalatesToAsk(t *testing.T) {
	p := NewPolicy(t.TempDir())
	for _, args := range []map[string]any{
		{"path": ".env"},
		{"path": ".env.local"},
		{"path": "private/key.pem"},
		{"path": ".ssh/id_rsa"},
		{"path": ".aws/credentials"},
	} {
		a := p.Classify("read_file", args)
		v := p.Decide(a, "")
		if v.Decision != Ask {
			t.Errorf("read_file %v decision = %v, want Ask", args["path"], v.Decision)
		}
		if v.Class != ClassHighRisk {
			t.Errorf("read_file %v class = %v, want HighRisk", args["path"], v.Class)
		}

		a = p.Classify("write_file", args)
		v = p.Decide(a, "")
		if v.Decision != Ask {
			t.Errorf("write_file %v decision = %v, want Ask", args["path"], v.Decision)
		}
	}

	// A normal file stays allowed for reads.
	a := p.Classify("read_file", map[string]any{"path": "src/main.go"})
	if v := p.Decide(a, ""); v.Decision != Allow {
		t.Errorf("normal read decision = %v, want Allow", v.Decision)
	}
}
