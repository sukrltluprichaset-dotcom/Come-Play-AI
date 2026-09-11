package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/auth"
)

const (
	UserIDKey   = "userID"
	UserRoleKey = "userRole"
)

// RequireAuth ตรวจสอบ JWT token จาก header Authorization แล้วฝากข้อมูล user
// ไว้ใน fiber.Ctx (ผ่าน c.Locals) ให้ handler ถัดไปดึงไปใช้ได้
func RequireAuth(jwtSecret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			return writeUnauthorized(c)
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := auth.ParseToken(tokenString, jwtSecret)
		if err != nil {
			return writeUnauthorized(c)
		}

		c.Locals(UserIDKey, claims.UserID)
		c.Locals(UserRoleKey, claims.Role)
		return c.Next()
	}
}

// RequireAdmin ใช้ต่อจาก RequireAuth เสมอ (ต้องรู้ตัวตนก่อนถึงจะเช็ค role ได้)
// เช็คว่า role ใน JWT token เป็น "admin" เท่านั้นถึงจะผ่าน
func RequireAdmin(c *fiber.Ctx) error {
	role, ok := c.Locals(UserRoleKey).(string)
	if !ok || role != "admin" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "ต้องเป็นผู้ดูแลระบบเท่านั้นถึงจะเข้าถึงส่วนนี้ได้",
		})
	}
	return c.Next()
}

func writeUnauthorized(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error": "กรุณาเข้าสู่ระบบก่อนใช้งาน",
	})
}
