package handlers

import (
	"database/sql"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/lib/pq"

	"comeplayai-backend/internal/auth"
	"comeplayai-backend/internal/models"
)

type AuthHandler struct {
	DB        *sql.DB
	JWTSecret string
}

func NewAuthHandler(db *sql.DB, jwtSecret string) *AuthHandler {
	return &AuthHandler{DB: db, JWTSecret: jwtSecret}
}

type authResponse struct {
	User  models.User `json:"user"`
	Token string      `json:"token"`
}

func writeJSON(c *fiber.Ctx, status int, payload interface{}) error {
	return c.Status(status).JSON(payload)
}

func writeError(c *fiber.Ctx, status int, message string) error {
	return writeJSON(c, status, fiber.Map{"error": message})
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var req registerRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if len(req.Username) < 3 || len(req.Username) > 50 {
		return writeError(c, fiber.StatusBadRequest, "ชื่อผู้ใช้ต้องมีความยาว 3-50 ตัวอักษร")
	}
	if !strings.Contains(req.Email, "@") {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบอีเมลไม่ถูกต้อง")
	}
	if len(req.Password) < 8 {
		return writeError(c, fiber.StatusBadRequest, "รหัสผ่านต้องมีอย่างน้อย 8 ตัวอักษร")
	}

	var exists bool
	err := h.DB.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 OR email = $2)`,
		req.Username, req.Email,
	).Scan(&exists)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if exists {
		return writeError(c, fiber.StatusConflict, "อีเมลหรือชื่อผู้ใช้งานนี้มีในระบบแล้ว")
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	defer tx.Rollback()

	var user models.User
	err = tx.QueryRow(
		`INSERT INTO users (username, email, password, role)
		 VALUES ($1, $2, $3, 'user')
		 RETURNING user_id, username, email, role, created_at`,
		req.Username, req.Email, hashedPassword,
	).Scan(&user.UserID, &user.Username, &user.Email, &user.Role, &user.CreatedAt)
	if err != nil {
		// เช็คว่า error เกิดจากอีเมล/ชื่อผู้ใช้ซ้ำ (unique constraint) หรือเปล่า เผื่อเคสมีคำขอสมัครสมาชิก
		// สองรอบมาพร้อมกันพอดี (เช่นกดปุ่มซ้ำซ้อน) แล้วเช็ค "มีอยู่แล้วมั้ย" ด้านบนผ่านไปพร้อมกันทั้งคู่
		// ก่อนที่รอบแรกจะ insert เสร็จ ถ้าเป็นแบบนี้ให้ตอบข้อความที่ตรงกับสาเหตุจริง แทนข้อความ error ทั่วไป
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return writeError(c, fiber.StatusConflict, "อีเมลหรือชื่อผู้ใช้งานนี้มีในระบบแล้ว")
		}
		return writeError(c, fiber.StatusInternalServerError, "สมัครสมาชิกไม่สำเร็จ")
	}

	if _, err := tx.Exec(`INSERT INTO coins (balance, user_id) VALUES (0, $1)`, user.UserID); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "สมัครสมาชิกไม่สำเร็จ")
	}

	if err := tx.Commit(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "สมัครสมาชิกไม่สำเร็จ")
	}

	token, err := auth.GenerateToken(user.UserID, user.Role, h.JWTSecret)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	return writeJSON(c, fiber.StatusCreated, authResponse{User: user, Token: token})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	var user models.User
	var passwordHash string
	var isSuspended bool
	err := h.DB.QueryRow(
		`SELECT user_id, username, email, password, role, created_at, is_suspended
		 FROM users WHERE email = $1 OR username = $1`,
		req.Email,
	).Scan(&user.UserID, &user.Username, &user.Email, &passwordHash, &user.Role, &user.CreatedAt, &isSuspended)

	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusUnauthorized, "ไม่พบข้อมูลผู้ใช้งาน หรือรหัสผ่านไม่ถูกต้อง")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	if !auth.CheckPassword(req.Password, passwordHash) {
		return writeError(c, fiber.StatusUnauthorized, "ไม่พบข้อมูลผู้ใช้งาน หรือรหัสผ่านไม่ถูกต้อง")
	}

	if isSuspended {
		return writeError(c, fiber.StatusForbidden, "บัญชีนี้ถูกระงับการใช้งาน กรุณาติดต่อผู้ดูแลระบบ")
	}

	_, _ = h.DB.Exec(`UPDATE users SET last_login = now() WHERE user_id = $1`, user.UserID)

	token, err := auth.GenerateToken(user.UserID, user.Role, h.JWTSecret)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	return writeJSON(c, fiber.StatusOK, authResponse{User: user, Token: token})
}

// ----- Change Password -----

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (h *AuthHandler) ChangePassword(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var req changePasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	if len(req.NewPassword) < 8 {
		return writeError(c, fiber.StatusBadRequest, "รหัสผ่านใหม่ต้องมีอย่างน้อย 8 ตัวอักษร")
	}

	var currentHash string
	err := h.DB.QueryRow(`SELECT password FROM users WHERE user_id = $1`, userID).Scan(&currentHash)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	if !auth.CheckPassword(req.OldPassword, currentHash) {
		return writeError(c, fiber.StatusUnauthorized, "รหัสผ่านเดิมไม่ถูกต้อง")
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	if _, err := h.DB.Exec(`UPDATE users SET password = $1 WHERE user_id = $2`, newHash, userID); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เปลี่ยนรหัสผ่านไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "เปลี่ยนรหัสผ่านสำเร็จ"})
}
