package config

import "testing"

func TestValidate(t *testing.T) {
	cfg := Config{
		SFTPGoAdminUser: "admin-user",
		SFTPGoAdminPass: "admin-pass",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}

	cfg.S3Endpoint = "http://minio:9000"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() expected S3 credential error")
	}
}
