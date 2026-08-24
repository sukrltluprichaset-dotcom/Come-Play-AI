package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

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

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	if len(req.Username) < 3 || len(req.Username) > 50 {
		writeError(w, http.StatusBadRequest, "ชื่อผู้ใช้ต้องมีความยาว 3-50 ตัวอักษร")
		return
	}
	if !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "รูปแบบอีเมลไม่ถูกต้อง")
		return
	}
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "รหัสผ่านต้องมีอย่างน้อย 8 ตัวอักษร")
		return
	}

	var exists bool
	err := h.DB.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 OR email = $2)`,
		req.Username, req.Email,
	).Scan(&exists)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "อีเมลหรือชื่อผู้ใช้งานนี้มีในระบบแล้ว")
		return
	}

	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
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
		writeError(w, http.StatusInternalServerError, "สมัครสมาชิกไม่สำเร็จ")
		return
	}

	if _, err := tx.Exec(`INSERT INTO coins (balance, user_id) VALUES (0, $1)`, user.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "สมัครสมาชิกไม่สำเร็จ")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "สมัครสมาชิกไม่สำเร็จ")
		return
	}

	token, err := auth.GenerateToken(user.UserID, user.Role, h.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{User: user, Token: token})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	var user models.User
	var passwordHash string
	err := h.DB.QueryRow(
		`SELECT user_id, username, email, password, role, created_at
		 FROM users WHERE email = $1 OR username = $1`,
		req.Email,
	).Scan(&user.UserID, &user.Username, &user.Email, &passwordHash, &user.Role, &user.CreatedAt)

	if err == sql.ErrNoRows {
		writeError(w, http.StatusUnauthorized, "ไม่พบข้อมูลผู้ใช้งาน หรือรหัสผ่านไม่ถูกต้อง")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}

	if !auth.CheckPassword(req.Password, passwordHash) {
		writeError(w, http.StatusUnauthorized, "ไม่พบข้อมูลผู้ใช้งาน หรือรหัสผ่านไม่ถูกต้อง")
		return
	}

	_, _ = h.DB.Exec(`UPDATE users SET last_login = now() WHERE user_id = $1`, user.UserID)

	token, err := auth.GenerateToken(user.UserID, user.Role, h.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{User: user, Token: token})
}

// ----- Change Password -----

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}

	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "รหัสผ่านใหม่ต้องมีอย่างน้อย 8 ตัวอักษร")
		return
	}

	var currentHash string
	err := h.DB.QueryRow(`SELECT password FROM users WHERE user_id = $1`, userID).Scan(&currentHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}

	if !auth.CheckPassword(req.OldPassword, currentHash) {
		writeError(w, http.StatusUnauthorized, "รหัสผ่านเดิมไม่ถูกต้อง")
		return
	}

	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}

	if _, err := h.DB.Exec(`UPDATE users SET password = $1 WHERE user_id = $2`, newHash, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "เปลี่ยนรหัสผ่านไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "เปลี่ยนรหัสผ่านสำเร็จ"})
}
