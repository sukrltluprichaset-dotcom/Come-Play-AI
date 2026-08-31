package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	AppPort            string
	DBHost             string
	DBPort             string
	DBUser             string
	DBPassword         string
	DBName             string
	DBSSLMode          string
	JWTSecret          string
	GeminiAPIKey       string
	SupabaseURL        string
	SupabaseServiceKey string
	SupabaseBucket     string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		AppPort:            getEnv("APP_PORT", "8080"),
		DBHost:             getEnv("DB_HOST", "localhost"),
		DBPort:             getEnv("DB_PORT", "5432"),
		DBUser:             getEnv("DB_USER", "postgres"),
		DBPassword:         os.Getenv("DB_PASSWORD"),
		DBName:             getEnv("DB_NAME", "comeplayai"),
		DBSSLMode:          getEnv("DB_SSLMODE", "disable"),
		JWTSecret:          getEnv("JWT_SECRET", "dev-secret-change-me-in-production"),
		GeminiAPIKey:       getEnv("GEMINI_API_KEY", ""),
		SupabaseURL:        getEnv("https://dgdashzusbhnpluwimji.supabase.co", ""),
		SupabaseServiceKey: getEnv("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6ImRnZGFzaHp1c2JobnBsdXdpbWppIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImlhdCI6MTc4NzU3ODc2MCwiZXhwIjoyMTAzMTU0NzYwfQ.XJPKUOMmMudTCjwZooLLOwdzVa3RPRQaJmKJJenXC7k", ""),
		SupabaseBucket:     getEnv("SUPABASE_BUCKET", "avatars"),
	}

	if cfg.DBPassword == "" {
		return nil, fmt.Errorf("DB_PASSWORD is not set (กรุณาใส่รหัสผ่าน PostgreSQL ในไฟล์ .env)")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
