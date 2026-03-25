package config

import (
	"fmt"
	"os"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	ListenAddr string
	DBPath     string
	DataDir    string

	SFTPGoURL       string
	SFTPGoAdminUser string
	SFTPGoAdminPass string
	BootstrapToken  string
	S3Bucket        string
	S3Region        string
	S3Endpoint      string
	S3AccessKey     string
	S3SecretKey     string
	S3UseSSL        bool
}

// Load reads configuration from environment variables.
func Load() Config {
	return Config{
		ListenAddr:      envOr("LISTEN_ADDR", ":9090"),
		DBPath:          envOr("DB_PATH", "sftpgo.db"),
		DataDir:         envOr("DATA_DIR", "/srv/sftpgo/data"),
		SFTPGoURL:       envOr("SFTPGO_URL", "http://localhost:8080"),
		SFTPGoAdminUser: envOr("SFTPGO_ADMIN_USER", ""),
		SFTPGoAdminPass: envOr("SFTPGO_ADMIN_PASS", ""),
		BootstrapToken:  envOr("BOOTSTRAP_TOKEN", ""),
		S3Bucket:        envOr("S3_BUCKET", "sftpgo"),
		S3Region:        envOr("S3_REGION", "us-east-1"),
		S3Endpoint:      envOr("S3_ENDPOINT", ""),
		S3AccessKey:     envOr("S3_ACCESS_KEY", ""),
		S3SecretKey:     envOr("S3_SECRET_KEY", ""),
		S3UseSSL:        os.Getenv("S3_USE_SSL") == "true",
	}
}

// Validate reports whether the runtime configuration is usable.
func (c Config) Validate() error {
	if c.SFTPGoAdminUser == "" || c.SFTPGoAdminPass == "" {
		return fmt.Errorf("SFTPGO_ADMIN_USER and SFTPGO_ADMIN_PASS are required")
	}
	if c.S3Endpoint != "" && (c.S3AccessKey == "" || c.S3SecretKey == "") {
		return fmt.Errorf("S3 credentials are required when S3_ENDPOINT is set")
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
