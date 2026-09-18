package handlers

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/lib/pq"

	"comeplayai-backend/internal/auth"
	"comeplayai-backend/internal/models"
)

// ----- ระบบชวนเพื่อน (Referral) -----

// charset กันสับสน (ไม่มี 0/O, 1/I/L) ให้เข้าชุดเดียวกับ CAPTCHA ที่ใช้ในหน้าเว็บอยู่แล้ว
const referralCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
const referralCodeLength = 6

// รางวัลตอนโค้ดชวนเพื่อนถูกใช้ตอนสมัครสมาชิกสำเร็จ
const referralRewardToReferrer = 50
const referralBonusToReferred = 20

func randomReferralCode() (string, error) {
	code := make([]byte, referralCodeLength)
	for i := range code {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(referralCodeCharset))))
		if err != nil {
			return "", err
		}
		code[i] = referralCodeCharset[n.Int64()]
	}
	return string(code), nil
}

// generateUniqueReferralCode สุ่มโค้ดชวนเพื่อนที่ไม่ซ้ำกับที่มีอยู่แล้วในระบบ (ลองใหม่ถ้าสุ่มซ้ำ)
func (h *AuthHandler) generateUniqueReferralCode() (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		code, err := randomReferralCode()
		if err != nil {
			return "", err
		}
		var exists bool
		if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE referral_code = $1)`, code).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", fmt.Errorf("ไม่สามารถสร้างโค้ดชวนเพื่อนที่ไม่ซ้ำได้")
}

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
	Username     string `json:"username"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	ReferralCode string `json:"referral_code"`
}

func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var req registerRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.ReferralCode = strings.ToUpper(strings.TrimSpace(req.ReferralCode))

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
	var avatarURL sql.NullString
	err = tx.QueryRow(
		`INSERT INTO users (username, email, password, role)
		 VALUES ($1, $2, $3, 'user')
		 RETURNING user_id, username, email, role, created_at, avatar_url`,
		req.Username, req.Email, hashedPassword,
	).Scan(&user.UserID, &user.Username, &user.Email, &user.Role, &user.CreatedAt, &avatarURL)
	if avatarURL.Valid {
		user.AvatarURL = &avatarURL.String
	}
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

	// ถ้ามีการกรอกโค้ดชวนเพื่อนมาด้วย: หาเจ้าของโค้ด ให้รางวัลทั้งสองฝ่าย แล้วบันทึกประวัติ
	// ถ้าโค้ดไม่ถูกต้อง/ไม่พบ/หรือเป็นโค้ดของตัวเอง จะไม่ทำอะไรและไม่แจ้ง error ใดๆ (สมัครสมาชิกสำเร็จตามปกติ)
	if req.ReferralCode != "" {
		var referrerID int64
		lookupErr := tx.QueryRow(`SELECT user_id FROM users WHERE referral_code = $1`, req.ReferralCode).Scan(&referrerID)
		if lookupErr == nil && referrerID != user.UserID {
			if _, err := tx.Exec(
				`INSERT INTO referrals (referrer_user_id, referred_user_id, reward_coins) VALUES ($1, $2, $3)`,
				referrerID, user.UserID, referralRewardToReferrer,
			); err == nil {
				_, _ = tx.Exec(`UPDATE coins SET balance = balance + $1 WHERE user_id = $2`, referralRewardToReferrer, referrerID)
				_, _ = tx.Exec(`UPDATE coins SET balance = balance + $1 WHERE user_id = $2`, referralBonusToReferred, user.UserID)
			}
		}
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
	var avatarURL sql.NullString
	err := h.DB.QueryRow(
		`SELECT user_id, username, email, password, role, created_at, is_suspended, avatar_url
		 FROM users WHERE email = $1 OR username = $1`,
		req.Email,
	).Scan(&user.UserID, &user.Username, &user.Email, &passwordHash, &user.Role, &user.CreatedAt, &isSuspended, &avatarURL)
	if avatarURL.Valid {
		user.AvatarURL = &avatarURL.String
	}

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

// ----- Update Profile (รูปโปรไฟล์) -----

type updateProfileRequest struct {
	AvatarURL string `json:"avatar_url"`
}

// UpdateProfile บันทึก URL รูปโปรไฟล์ใหม่ของผู้ใช้ (ไฟล์รูปเองอัปโหลดผ่าน /api/uploads ที่มีอยู่แล้ว
// endpoint นี้แค่รับ URL ที่ได้กลับมาไปบันทึกลงบัญชีผู้ใช้)
func (h *AuthHandler) UpdateProfile(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var req updateProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.AvatarURL = strings.TrimSpace(req.AvatarURL)
	if req.AvatarURL == "" {
		return writeError(c, fiber.StatusBadRequest, "ไม่พบ URL รูปโปรไฟล์")
	}

	var user models.User
	var avatarURL sql.NullString
	err := h.DB.QueryRow(
		`UPDATE users SET avatar_url = $1 WHERE user_id = $2
		 RETURNING user_id, username, email, role, created_at, avatar_url`,
		req.AvatarURL, userID,
	).Scan(&user.UserID, &user.Username, &user.Email, &user.Role, &user.CreatedAt, &avatarURL)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "บันทึกรูปโปรไฟล์ไม่สำเร็จ")
	}
	if avatarURL.Valid {
		user.AvatarURL = &avatarURL.String
	}

	return writeJSON(c, fiber.StatusOK, user)
}

// ----- Referral (ชวนเพื่อน) -----

// GetReferralInfo คืนโค้ดชวนเพื่อนของผู้ใช้ (สร้างให้อัตโนมัติถ้ายังไม่มี) พร้อมประวัติการชวนและเหรียญที่ได้รับ
func (h *AuthHandler) GetReferralInfo(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var code sql.NullString
	if err := h.DB.QueryRow(`SELECT referral_code FROM users WHERE user_id = $1`, userID).Scan(&code); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดข้อมูลชวนเพื่อนไม่สำเร็จ")
	}

	if !code.Valid || code.String == "" {
		newCode, err := h.generateUniqueReferralCode()
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "สร้างโค้ดชวนเพื่อนไม่สำเร็จ")
		}
		if _, err := h.DB.Exec(`UPDATE users SET referral_code = $1 WHERE user_id = $2`, newCode, userID); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "สร้างโค้ดชวนเพื่อนไม่สำเร็จ")
		}
		code = sql.NullString{String: newCode, Valid: true}
	}

	rows, err := h.DB.Query(
		`SELECT u.username, r.reward_coins, r.created_at
		 FROM referrals r
		 JOIN users u ON u.user_id = r.referred_user_id
		 WHERE r.referrer_user_id = $1
		 ORDER BY r.created_at DESC`,
		userID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการชวนเพื่อนไม่สำเร็จ")
	}
	defer rows.Close()

	info := models.ReferralInfo{ReferralCode: code.String, Referrals: []models.ReferralRecord{}}
	for rows.Next() {
		var rec models.ReferralRecord
		if err := rows.Scan(&rec.ReferredUsername, &rec.RewardCoins, &rec.CreatedAt); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการชวนเพื่อนไม่สำเร็จ")
		}
		info.TotalCoins += rec.RewardCoins
		info.Referrals = append(info.Referrals, rec)
	}
	info.TotalInvited = len(info.Referrals)

	return writeJSON(c, fiber.StatusOK, info)
}
