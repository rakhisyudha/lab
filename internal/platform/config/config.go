// Package config membaca dan memvalidasi konfigurasi dari environment.
//
// Secret (DATABASE_URL, JWT_SIGNING_KEY) hanya dibaca dari environment dan
// tidak pernah ditulis ke file atau pesan error (Requirement 18.6).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Nama variabel environment.
const (
	EnvDeploymentMode = "DEPLOYMENT_MODE"
	EnvRateLimitStore = "RATELIMIT_STORE"
	EnvQueueStore     = "QUEUE_STORE"
	EnvDatabaseURL    = "DATABASE_URL"
	EnvJWTSigningKey  = "JWT_SIGNING_KEY"
	EnvCORSOrigins    = "CORS_ALLOWED_ORIGINS"
	EnvSecretProvider = "SECRET_PROVIDER"
)

// DeploymentMode menentukan topologi deployment (D-15).
type DeploymentMode string

const (
	// ModeMultiInstance adalah default. State antrean dan rate limit wajib di PostgreSQL.
	ModeMultiInstance DeploymentMode = "multi_instance"
	// ModeSingleInstance mengizinkan penyimpanan in-process.
	ModeSingleInstance DeploymentMode = "single_instance"
	// ModeDev mengizinkan penyimpanan in-process untuk lingkungan pengembangan.
	ModeDev DeploymentMode = "dev"
)

// StoreKind adalah jenis penyimpanan untuk rate limit atau antrean.
type StoreKind string

const (
	// StorePostgres memakai PostgreSQL sebagai penyimpanan bersama. Ini default.
	StorePostgres StoreKind = "postgres"
	// StoreMemory menyimpan state di memori proses. Hanya valid pada single_instance atau dev.
	StoreMemory StoreKind = "memory"
)

// minJWTKeyBytes adalah panjang minimum kunci HS256 (256 bit).
const minJWTKeyBytes = 32

// Secret adalah nilai rahasia. Tipe ini menyembunyikan isinya saat diformat
// dengan fmt, sehingga tidak bocor ke log secara tidak sengaja.
type Secret string

// String menyembunyikan nilai secret.
func (Secret) String() string { return "[REDACTED]" }

// GoString menyembunyikan nilai secret pada %#v.
func (Secret) GoString() string { return "config.Secret([REDACTED])" }

// Reveal mengembalikan nilai asli. Pemanggilan hanya di tempat yang benar-benar membutuhkannya.
func (s Secret) Reveal() string { return string(s) }

// Config adalah konfigurasi aplikasi yang sudah divalidasi.
type Config struct {
	DeploymentMode     DeploymentMode
	RateLimitStore     StoreKind
	QueueStore         StoreKind
	DatabaseURL        Secret
	JWTSigningKey      Secret
	CORSAllowedOrigins []string
	SecretProvider     string
}

// LoadDotEnv memuat berkas .env bila ada. Variabel yang sudah ada di environment
// processes tidak ditimpa, sehingga environment ( CI, container) tetap berlaku.
// Tidak ada berkas .env bukan error.
func LoadDotEnv() error {
	err := godotenv.Load()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Load membaca konfigurasi dari environment proses.
func Load() (Config, error) {
	return Parse(os.LookupEnv)
}

// Parse membaca dan memvalidasi konfigurasi memakai lookup sebagai sumber nilai.
// Semua kesalahan validasi dikumpulkan sekaligus. Pesan error hanya menyebut nama
// variabel dan alasannya, tidak pernah menyertakan nilai secret.
func Parse(lookup func(key string) (string, bool)) (Config, error) {
	get := func(key string) string {
		v, _ := lookup(key)
		return strings.TrimSpace(v)
	}

	var errs []error
	cfg := Config{SecretProvider: "env"}

	// DEPLOYMENT_MODE
	cfg.DeploymentMode = DeploymentMode(orDefault(get(EnvDeploymentMode), string(ModeMultiInstance)))
	switch cfg.DeploymentMode {
	case ModeMultiInstance, ModeSingleInstance, ModeDev:
	default:
		errs = append(errs, fmt.Errorf("%s: nilai %q tidak dikenal (multi_instance, single_instance, dev)", EnvDeploymentMode, cfg.DeploymentMode))
	}

	// RATELIMIT_STORE dan QUEUE_STORE
	var err error
	if cfg.RateLimitStore, err = parseStore(EnvRateLimitStore, get(EnvRateLimitStore)); err != nil {
		errs = append(errs, err)
	}
	if cfg.QueueStore, err = parseStore(EnvQueueStore, get(EnvQueueStore)); err != nil {
		errs = append(errs, err)
	}
	if cfg.DeploymentMode == ModeMultiInstance {
		if cfg.RateLimitStore == StoreMemory {
			errs = append(errs, fmt.Errorf("%s=memory tidak diizinkan pada %s=multi_instance: state bucket tidak dibagi antar instance", EnvRateLimitStore, EnvDeploymentMode))
		}
		if cfg.QueueStore == StoreMemory {
			errs = append(errs, fmt.Errorf("%s=memory tidak diizinkan pada %s=multi_instance: antrean tidak dibagi antar instance", EnvQueueStore, EnvDeploymentMode))
		}
	}

	// DATABASE_URL (secret)
	cfg.DatabaseURL = Secret(get(EnvDatabaseURL))
	if err := validateDatabaseURL(cfg.DatabaseURL.Reveal()); err != nil {
		errs = append(errs, err)
	}

	// JWT_SIGNING_KEY (secret)
	cfg.JWTSigningKey = Secret(get(EnvJWTSigningKey))
	switch {
	case cfg.JWTSigningKey == "":
		errs = append(errs, fmt.Errorf("%s: wajib diisi", EnvJWTSigningKey))
	case len(cfg.JWTSigningKey) < minJWTKeyBytes:
		errs = append(errs, fmt.Errorf("%s: minimal %d byte untuk HS256", EnvJWTSigningKey, minJWTKeyBytes))
	}

	// CORS_ALLOWED_ORIGINS: daftar dipisah koma. Kosong berarti tidak ada origin yang diizinkan.
	origins, originErrs := parseOrigins(get(EnvCORSOrigins))
	cfg.CORSAllowedOrigins = origins
	errs = append(errs, originErrs...)

	// SECRET_PROVIDER: P0 hanya mendukung environment (Requirement 18.6).
	if p := get(EnvSecretProvider); p != "" {
		cfg.SecretProvider = p
	}
	if cfg.SecretProvider != "env" {
		errs = append(errs, fmt.Errorf("%s: nilai %q belum didukung pada P0 (hanya env)", EnvSecretProvider, cfg.SecretProvider))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("konfigurasi tidak valid: %w", errors.Join(errs...))
	}
	return cfg, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseStore(name, raw string) (StoreKind, error) {
	switch StoreKind(orDefault(raw, string(StorePostgres))) {
	case StorePostgres:
		return StorePostgres, nil
	case StoreMemory:
		return StoreMemory, nil
	default:
		return "", fmt.Errorf("%s: nilai %q tidak dikenal (postgres, memory)", name, raw)
	}
}

// validateDatabaseURL memeriksa skema dan host. Pesan error tidak menyertakan URL,
// karena URL berisi kredensial. Error dari url.Parse juga tidak dipakai untuk alasan
// yang sama.
func validateDatabaseURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("%s: wajib diisi", EnvDatabaseURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: format URL tidak valid", EnvDatabaseURL)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("%s: skema harus postgres:// atau postgresql://", EnvDatabaseURL)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: host database kosong", EnvDatabaseURL)
	}
	return nil
}

// parseOrigins memvalidasi daftar origin CORS. Setiap origin harus berbentuk
// scheme://host[:port] tanpa path, query, atau wildcard.
func parseOrigins(raw string) ([]string, []error) {
	var (
		origins []string
		errs    []error
	)
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" ||
			strings.Contains(origin, "*") {
			errs = append(errs, fmt.Errorf("%s: origin tidak valid, gunakan scheme://host[:port] tanpa path atau wildcard", EnvCORSOrigins))
			continue
		}
		origins = append(origins, origin)
	}
	return origins, errs
}
