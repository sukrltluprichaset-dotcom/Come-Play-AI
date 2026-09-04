package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"comeplayai-backend/internal/models"
)

type AdminHandler struct {
	DB *sql.DB
}

func NewAdminHandler(db *sql.DB) *AdminHandler {
	return &AdminHandler{DB: db}
}

// ----- ดูรายชื่อผู้ใช้ทั้งหมด -----

type adminUserView struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	Balance   int    `json:"balance"`
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(
		`SELECT u.user_id, u.username, u.email, u.role, u.created_at, COALESCE(c.balance, 0)
		 FROM users u
		 LEFT JOIN coins c ON c.user_id = u.user_id
		 ORDER BY u.user_id ASC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดรายชื่อผู้ใช้ไม่สำเร็จ")
		return
	}
	defer rows.Close()

	users := []adminUserView{}
	for rows.Next() {
		var u adminUserView
		if err := rows.Scan(&u.UserID, &u.Username, &u.Email, &u.Role, &u.CreatedAt, &u.Balance); err != nil {
			writeError(w, http.StatusInternalServerError, "โหลดรายชื่อผู้ใช้ไม่สำเร็จ")
			return
		}
		users = append(users, u)
	}

	writeJSON(w, http.StatusOK, users)
}

// ----- เติมเหรียญแบบ Manual โดยแอดมิน -----

type adjustCoinsRequest struct {
	Amount int `json:"amount"` // ใส่ค่าบวกเพื่อเติม ใส่ค่าลบเพื่อหัก
}

// ----- ระงับ/ยกเลิกระงับบัญชีผู้ใช้ -----

type suspendUserRequest struct {
	Suspended bool `json:"suspended"`
}

func (h *AdminHandler) SuspendUser(w http.ResponseWriter, r *http.Request) {
	targetUserID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสผู้ใช้ไม่ถูกต้อง")
		return
	}

	var req suspendUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}

	result, err := h.DB.Exec(`UPDATE users SET is_suspended = $1 WHERE user_id = $2`, req.Suspended, targetUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "อัปเดตสถานะไม่สำเร็จ")
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "ไม่พบผู้ใช้นี้")
		return
	}

	message := "ระงับบัญชีสำเร็จ"
	if !req.Suspended {
		message = "ยกเลิกการระงับบัญชีสำเร็จ"
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": message})
}

// ----- ลบบัญชีผู้ใช้ถาวร -----

func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	targetUserID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสผู้ใช้ไม่ถูกต้อง")
		return
	}

	adminID := userIDFromContext(r)
	if adminID == targetUserID {
		writeError(w, http.StatusBadRequest, "ไม่สามารถลบบัญชีของตัวเองได้")
		return
	}

	result, err := h.DB.Exec(`DELETE FROM users WHERE user_id = $1`, targetUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ลบบัญชีไม่สำเร็จ (อาจมีข้อมูลเชื่อมโยงอยู่)")
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "ไม่พบผู้ใช้นี้")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "ลบบัญชีผู้ใช้สำเร็จ"})
}

func (h *AdminHandler) AdjustCoins(w http.ResponseWriter, r *http.Request) {
	targetUserID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสผู้ใช้ไม่ถูกต้อง")
		return
	}

	var req adjustCoinsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}
	if req.Amount == 0 {
		writeError(w, http.StatusBadRequest, "กรุณาระบุจำนวนเหรียญที่ไม่เป็นศูนย์")
		return
	}

	var newBalance int
	err = h.DB.QueryRow(
		`UPDATE coins SET balance = balance + $1 WHERE user_id = $2 AND balance + $1 >= 0 RETURNING balance`,
		req.Amount, targetUserID,
	).Scan(&newBalance)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusBadRequest, "ยอดเหรียญจะติดลบไม่ได้ หรือไม่พบผู้ใช้นี้")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "ปรับยอดเหรียญไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user_id":     targetUserID,
		"new_balance": newBalance,
	})
}

// ----- ดูตัวละครทั้งหมดในระบบ -----

func (h *AdminHandler) ListAllCharacters(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(
		`SELECT c.character_id, c.name, c.personality, c.is_shared, c.usage_count, c.rating, c.user_id, u.username
		 FROM characters c
		 JOIN users u ON u.user_id = c.user_id
		 ORDER BY c.character_id DESC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดรายชื่อตัวละครไม่สำเร็จ")
		return
	}
	defer rows.Close()

	type adminCharView struct {
		CharacterID int64   `json:"character_id"`
		Name        string  `json:"name"`
		Personality string  `json:"personality"`
		IsShared    bool    `json:"is_shared"`
		UsageCount  int     `json:"usage_count"`
		Rating      float64 `json:"rating"`
		OwnerID     int64   `json:"owner_id"`
		OwnerName   string  `json:"owner_name"`
	}

	characters := []adminCharView{}
	for rows.Next() {
		var c adminCharView
		if err := rows.Scan(&c.CharacterID, &c.Name, &c.Personality, &c.IsShared, &c.UsageCount, &c.Rating, &c.OwnerID, &c.OwnerName); err != nil {
			writeError(w, http.StatusInternalServerError, "โหลดรายชื่อตัวละครไม่สำเร็จ")
			return
		}
		characters = append(characters, c)
	}

	writeJSON(w, http.StatusOK, characters)
}

// ----- แอดมินลบตัวละครใดก็ได้ -----

func (h *AdminHandler) DeleteCharacter(w http.ResponseWriter, r *http.Request) {
	characterID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
		return
	}

	result, err := h.DB.Exec(`DELETE FROM characters WHERE character_id = $1`, characterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ลบตัวละครไม่สำเร็จ")
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "ไม่พบตัวละครนี้")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "ลบตัวละครสำเร็จ"})
}

// ----- ดูรายงานทั้งหมด -----

func (h *AdminHandler) ListReports(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(
		`SELECT r.report_id, r.details, r.status, r.character_id, c.name, r.user_id, u.username, r.created_at
		 FROM reports r
		 JOIN characters c ON c.character_id = r.character_id
		 JOIN users u ON u.user_id = r.user_id
		 ORDER BY r.created_at DESC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดรายงานไม่สำเร็จ")
		return
	}
	defer rows.Close()

	reports := []models.ReportAdminView{}
	for rows.Next() {
		var rp models.ReportAdminView
		if err := rows.Scan(&rp.ReportID, &rp.Details, &rp.Status, &rp.CharacterID, &rp.CharacterName, &rp.UserID, &rp.Username, &rp.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "โหลดรายงานไม่สำเร็จ")
			return
		}
		reports = append(reports, rp)
	}

	writeJSON(w, http.StatusOK, reports)
}

// ----- ปิดเคสรายงาน -----

type updateReportStatusRequest struct {
	Status string `json:"status"` // "resolved" หรือ "rejected"
}

func (h *AdminHandler) UpdateReportStatus(w http.ResponseWriter, r *http.Request) {
	reportID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสรายงานไม่ถูกต้อง")
		return
	}

	var req updateReportStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}
	if req.Status != "resolved" && req.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "สถานะต้องเป็น resolved หรือ rejected เท่านั้น")
		return
	}

	result, err := h.DB.Exec(`UPDATE reports SET status = $1 WHERE report_id = $2`, req.Status, reportID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "อัปเดตสถานะไม่สำเร็จ")
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeError(w, http.StatusNotFound, "ไม่พบรายงานนี้")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "อัปเดตสถานะสำเร็จ"})
}

// ----- สถิติภาพรวมระบบ -----

func (h *AdminHandler) Stats(w http.ResponseWriter, r *http.Request) {
	var totalUsers, totalCharacters, totalChats, pendingReports int

	h.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalUsers)
	h.DB.QueryRow(`SELECT COUNT(*) FROM characters`).Scan(&totalCharacters)
	h.DB.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&totalChats)
	h.DB.QueryRow(`SELECT COUNT(*) FROM reports WHERE status = 'pending'`).Scan(&pendingReports)

	writeJSON(w, http.StatusOK, map[string]int{
		"total_users":      totalUsers,
		"total_characters": totalCharacters,
		"total_chats":      totalChats,
		"pending_reports":  pendingReports,
	})
}
