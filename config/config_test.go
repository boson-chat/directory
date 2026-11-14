package config

import "testing"

func TestFakeVerifierRefusedInProduction(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("DNS_VERIFIER", "fake")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("fake verifier accepted in production")
	}
	t.Setenv("ENV", "development")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VerifyConfig.LapseAfter.Hours() != 336 || cfg.HTTPConfig.PublicURL != "http://localhost:8080" {
		t.Fatalf("defaults %+v", cfg.VerifyConfig)
	}
	t.Setenv("DIRECTORY_PUBLIC_URL", "https://d.example/")
	t.Setenv("AUTH_ISSUER", "https://auth.example/")
	cfg, _ = LoadConfig()
	if cfg.HTTPConfig.PublicURL != "https://d.example" || cfg.AuthConfig.JWKSURL != "https://auth.example/.well-known/jwks.json" || cfg.MediaConfig.PublicURL != "https://d.example/media" {
		t.Fatalf("derived %+v %+v", cfg.HTTPConfig, cfg.AuthConfig)
	}
}
