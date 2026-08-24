package database

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"

	"comeplayai-backend/internal/config"
)

// Connect เชื่อมต่อ PostgreSQL ตามค่าที่ได้จาก config แล้ว ping เพื่อยืนยันว่าต่อได้จริง
func Connect(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("เปิดการเชื่อมต่อไม่สำเร็จ: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping ไม่ผ่าน: %w", err)
	}

	return db, nil
}

// PackageCount นับจำนวนแถวในตาราง packages (ใช้เช็คว่าต่อฐานข้อมูลจริงติด)
func PackageCount(db *sql.DB) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM packages").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
