package cmd

import (
	"testing"
)

func TestBuildServeOptions(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantMode string
		wantErr  bool
		minOpts  int // minimum number of options expected
	}{
		{
			name:     "default no auth",
			env:      map[string]string{},
			wantMode: "none",
		},
		{
			name:     "explicit none",
			env:      map[string]string{envAuthMode: "none"},
			wantMode: "none",
		},
		{
			name:     "jwt with secret",
			env:      map[string]string{envAuthMode: "jwt", envJWTSecret: "s3cr3t"},
			wantMode: "jwt",
			minOpts:  1,
		},
		{
			name:     "jwt with jwks url",
			env:      map[string]string{envAuthMode: "jwt", envJWTJWKSURL: "https://issuer/.well-known/jwks.json"},
			wantMode: "jwt",
			minOpts:  1,
		},
		{
			name:    "jwt missing secret and jwks",
			env:     map[string]string{envAuthMode: "jwt"},
			wantErr: true,
		},
		{
			name:     "apikey with entries",
			env:      map[string]string{envAuthMode: "apikey", envAPIKeys: "k1:admin:t1,k2"},
			wantMode: "apikey",
			minOpts:  1,
		},
		{
			name:    "apikey empty list",
			env:     map[string]string{envAuthMode: "apikey", envAPIKeys: ""},
			wantErr: true,
		},
		{
			name:    "apikey empty key segment",
			env:     map[string]string{envAuthMode: "apikey", envAPIKeys: ":admin"},
			wantErr: true,
		},
		{
			name:    "invalid mode",
			env:     map[string]string{envAuthMode: "bogus"},
			wantErr: true,
		},
		{
			name:     "cors origins with no auth",
			env:      map[string]string{envCORSOrigins: "https://a.example, https://b.example"},
			wantMode: "none",
			minOpts:  1,
		},
		{
			name:     "case-insensitive mode",
			env:      map[string]string{envAuthMode: "JWT", envJWTSecret: "x"},
			wantMode: "jwt",
			minOpts:  1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, mode, err := buildServeOptions(tc.env)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (mode=%q)", mode)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != tc.wantMode {
				t.Errorf("mode: got %q, want %q", mode, tc.wantMode)
			}
			if len(opts) < tc.minOpts {
				t.Errorf("options: got %d, want at least %d", len(opts), tc.minOpts)
			}
		})
	}
}

func TestParseAPIKeys(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantLen int
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"single bare key defaults role", "abc", 1, false},
		{"key with role and tenant", "abc:admin:acme", 1, false},
		{"empty key segment", ":admin", 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			keys, err := parseAPIKeys(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(keys) != tc.wantLen {
				t.Errorf("got %d keys, want %d", len(keys), tc.wantLen)
			}
		})
	}

	// Verify default role and explicit role/tenant parsing.
	keys, err := parseAPIKeys("bare,scoped:viewer:tenantX")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry := keys["bare"]; entry.Scope != "user" {
		t.Errorf("bare key: got scope %q, want %q", entry.Scope, "user")
	}
	if entry := keys["scoped"]; entry.Scope != "viewer" || entry.TenantID != "tenantX" {
		t.Errorf("scoped key: got scope=%q tenant=%q, want viewer/tenantX", entry.Scope, entry.TenantID)
	}
}

func TestParseBool(t *testing.T) {
	tests := []struct {
		raw       string
		wantValue bool
		wantOK    bool
	}{
		{"true", true, true},
		{"1", true, true},
		{"On", true, true},
		{"false", false, true},
		{"0", false, true},
		{"no", false, true},
		{"", false, false},
		{"maybe", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			v, ok := parseBool(tc.raw)
			if v != tc.wantValue || ok != tc.wantOK {
				t.Errorf("parseBool(%q): got (%v,%v), want (%v,%v)", tc.raw, v, ok, tc.wantValue, tc.wantOK)
			}
		})
	}
}

// TestBuildServeOptionsSwaggerRBAC verifies the swagger/rbac toggles append
// options only when they diverge from the server defaults (swagger enabled,
// rbac disabled).
func TestBuildServeOptionsSwaggerRBAC(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		wantMinOpt int
	}{
		{"defaults append nothing", map[string]string{}, 0},
		{"swagger enabled is default (no opt)", map[string]string{"CHRONOS_SWAGGER": "true"}, 0},
		{"rbac false is default (no opt)", map[string]string{"CHRONOS_RBAC": "false"}, 0},
		{"disable swagger appends", map[string]string{"CHRONOS_SWAGGER": "false"}, 1},
		{"enable rbac appends", map[string]string{"CHRONOS_RBAC": "true"}, 1},
		{"both diverge appends two", map[string]string{"CHRONOS_SWAGGER": "off", "CHRONOS_RBAC": "on"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, _, err := buildServeOptions(tc.env)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(opts) != tc.wantMinOpt {
				t.Errorf("options: got %d, want %d", len(opts), tc.wantMinOpt)
			}
		})
	}
}
