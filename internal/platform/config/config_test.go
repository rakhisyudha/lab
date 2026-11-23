package config

import (
	"fmt"
	"strings"
	"testing"
)

const (
	validDBURL = "postgres://app:s3cret-pass@db.internal:5432/backendlab?sslmode=require"
	validKey   = "0123456789abcdef0123456789abcdef" // 32 byte
)

// env membangun lookup dari map. Nilai kosong dianggap tidak ada.
func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func baseEnv() map[string]string {
	return map[string]string{
		EnvDatabaseURL:   validDBURL,
		EnvJWTSigningKey: validKey,
	}
}

func with(base map[string]string, kv ...string) map[string]string {
	out := make(map[string]string, len(base)+len(kv)/2)
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(env(baseEnv()))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.DeploymentMode != ModeMultiInstance {
		t.Errorf("DeploymentMode = %q, want multi_instance", cfg.DeploymentMode)
	}
	if cfg.RateLimitStore != StorePostgres {
		t.Errorf("RateLimitStore = %q, want postgres", cfg.RateLimitStore)
	}
	if cfg.QueueStore != StorePostgres {
		t.Errorf("QueueStore = %q, want postgres", cfg.QueueStore)
	}
	if cfg.SecretProvider != "env" {
		t.Errorf("SecretProvider = %q, want env", cfg.SecretProvider)
	}
	if len(cfg.CORSAllowedOrigins) != 0 {
		t.Errorf("CORSAllowedOrigins = %v, want empty", cfg.CORSAllowedOrigins)
	}
}

func TestParseMemoryStoreRules(t *testing.T) {
	tests := []struct {
		name    string
		extra   map[string]string
		wantErr bool
	}{
		{"multi_instance dengan memory rate limit ditolak", map[string]string{EnvRateLimitStore: "memory"}, true},
		{"multi_instance dengan memory queue ditolak", map[string]string{EnvQueueStore: "memory"}, true},
		{"single_instance dengan memory diizinkan", map[string]string{EnvDeploymentMode: "single_instance", EnvRateLimitStore: "memory", EnvQueueStore: "memory"}, false},
		{"dev dengan memory diizinkan", map[string]string{EnvDeploymentMode: "dev", EnvRateLimitStore: "memory", EnvQueueStore: "memory"}, false},
		{"multi_instance dengan postgres diizinkan", map[string]string{EnvRateLimitStore: "postgres", EnvQueueStore: "postgres"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(env(with(baseEnv(), flatten(tt.extra)...)))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string // substring yang harus ada di pesan error
	}{
		{"mode tidak dikenal", with(baseEnv(), EnvDeploymentMode, "staging"), EnvDeploymentMode},
		{"store tidak dikenal", with(baseEnv(), EnvQueueStore, "redis"), EnvQueueStore},
		{"database url kosong", with(baseEnv(), EnvDatabaseURL, ""), EnvDatabaseURL},
		{"database url skema salah", with(baseEnv(), EnvDatabaseURL, "mysql://u:p@h/db"), EnvDatabaseURL},
		{"database url tanpa host", with(baseEnv(), EnvDatabaseURL, "postgres:///db"), EnvDatabaseURL},
		{"jwt key kosong", with(baseEnv(), EnvJWTSigningKey, ""), EnvJWTSigningKey},
		{"jwt key terlalu pendek", with(baseEnv(), EnvJWTSigningKey, "pendek"), EnvJWTSigningKey},
		{"cors origin wildcard", with(baseEnv(), EnvCORSOrigins, "*"), EnvCORSOrigins},
		{"cors origin dengan path", with(baseEnv(), EnvCORSOrigins, "https://app.example.com/admin"), EnvCORSOrigins},
		{"cors origin tanpa scheme", with(baseEnv(), EnvCORSOrigins, "app.example.com"), EnvCORSOrigins},
		{"secret provider belum didukung", with(baseEnv(), EnvSecretProvider, "vault"), EnvSecretProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(env(tt.env))
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q tidak menyebut %q", err, tt.want)
			}
		})
	}
}

func TestParseErrorDoesNotLeakSecrets(t *testing.T) {
	// URL dan key sengaja salah agar error validasi muncul.
	badURL := "postgres://app:TOPSECRET-PW@[::1:5432/backendlab"
	badKey := "KEY-RAHASIA-PENDEK"
	_, err := Parse(env(with(baseEnv(), EnvDatabaseURL, badURL, EnvJWTSigningKey, badKey)))
	if err == nil {
		t.Fatal("Parse() error = nil, want error")
	}
	for _, secret := range []string{"TOPSECRET-PW", badKey, "app:"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error mengandung secret %q: %v", secret, err)
		}
	}
}

func TestSecretIsRedactedWhenFormatted(t *testing.T) {
	cfg, err := Parse(env(baseEnv()))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	out := fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg)
	for _, secret := range []string{validDBURL, validKey, "s3cret-pass"} {
		if strings.Contains(out, secret) {
			t.Fatalf("format konfigurasi membocorkan secret %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("format konfigurasi tidak memuat [REDACTED]: %s", out)
	}
}

func TestParseCORSOrigins(t *testing.T) {
	cfg, err := Parse(env(with(baseEnv(), EnvCORSOrigins, " https://app.example.com , http://localhost:4321,,")))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []string{"https://app.example.com", "http://localhost:4321"}
	if strings.Join(cfg.CORSAllowedOrigins, "|") != strings.Join(want, "|") {
		t.Fatalf("CORSAllowedOrigins = %v, want %v", cfg.CORSAllowedOrigins, want)
	}
}

// flatten mengubah map menjadi pasangan kunci-nilai berurutan untuk with.
func flatten(m map[string]string) []string {
	out := make([]string, 0, len(m)*2)
	for k, v := range m {
		out = append(out, k, v)
	}
	return out
}
