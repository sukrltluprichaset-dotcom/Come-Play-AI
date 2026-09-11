package handlers

import (
	"database/sql"
	"strconv"

	"github.com/gofiber/fiber/v2"

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
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
	Balance     int    `json:"balance"`
	IsSuspended bool   `json:"is_suspended"`
}

func (h *AdminHandler) ListUsers(c *fiber.Ctx) error {
	rows, err := h.DB.Query(
		`SELECT u.user_id, u.username, u.email, u.role, u.created_at, COALESCE(c.balance, 0), u.is_suspended
		 FROM users u
		 LEFT JOIN coins c ON c.user_id = u.user_id
		 ORDER BY u.user_id ASC`,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายชื่อผู้ใช้ไม่สำเร็จ")
	}
	defer rows.Close()

	users := []adminUserView{}
	for rows.Next() {
		var u adminUserView
		if err := rows.Scan(&u.UserID, &u.Username, &u.Email, &u.Role, &u.CreatedAt, &u.Balance, &u.IsSuspended); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายชื่อผู้ใช้ไม่สำเร็จ")
		}
		users = append(users, u)
	}

	return writeJSON(c, fiber.StatusOK, users)
}

// ----- เติมเหรียญแบบ Manual โดยแอดมิน -----

type adjustCoinsRequest struct {
	Amount int `json:"amount"` // ใส่ค่าบวกเพื่อเติม ใส่ค่าลบเพื่อหัก
}

// ----- ระงับ/ยกเลิกระงับบัญชีผู้ใช้ -----

type suspendUserRequest struct {
	Suspended bool `json:"suspended"`
}

func (h *AdminHandler) SuspendUser(c *fiber.Ctx) error {
	targetUserID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสผู้ใช้ไม่ถูกต้อง")
	}

	var req suspendUserRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	result, err := h.DB.Exec(`UPDATE users SET is_suspended = $1 WHERE user_id = $2`, req.Suspended, targetUserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "อัปเดตสถานะไม่สำเร็จ")
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบผู้ใช้นี้")
	}

	message := "ระงับบัญชีสำเร็จ"
	if !req.Suspended {
		message = "ยกเลิกการระงับบัญชีสำเร็จ"
	}
	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": message})
}

// ----- ลบบัญชีผู้ใช้ถาวร -----

func (h *AdminHandler) DeleteUser(c *fiber.Ctx) error {
	targetUserID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสผู้ใช้ไม่ถูกต้อง")
	}

	adminID := userIDFromContext(c)
	if adminID == targetUserID {
		return writeError(c, fiber.StatusBadRequest, "ไม่สามารถลบบัญชีของตัวเองได้")
	}

	result, err := h.DB.Exec(`DELETE FROM users WHERE user_id = $1`, targetUserID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ลบบัญชีไม่สำเร็จ (อาจมีข้อมูลเชื่อมโยงอยู่)")
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบผู้ใช้นี้")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "ลบบัญชีผู้ใช้สำเร็จ"})
}

func (h *AdminHandler) AdjustCoins(c *fiber.Ctx) error {
	targetUserID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสผู้ใช้ไม่ถูกต้อง")
	}

	var req adjustCoinsRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}
	if req.Amount == 0 {
		return writeError(c, fiber.StatusBadRequest, "กรุณาระบุจำนวนเหรียญที่ไม่เป็นศูนย์")
	}

	var newBalance int
	err = h.DB.QueryRow(
		`UPDATE coins SET balance = balance + $1 WHERE user_id = $2 AND balance + $1 >= 0 RETURNING balance`,
		req.Amount, targetUserID,
	).Scan(&newBalance)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusBadRequest, "ยอดเหรียญจะติดลบไม่ได้ หรือไม่พบผู้ใช้นี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ปรับยอดเหรียญไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{
		"user_id":     targetUserID,
		"new_balance": newBalance,
	})
}

// ----- ดูตัวละครทั้งหมดในระบบ -----

func (h *AdminHandler) ListAllCharacters(c *fiber.Ctx) error {
	rows, err := h.DB.Query(
		`SELECT c.character_id, c.name, c.personality, c.is_shared, c.usage_count, c.rating, c.user_id, u.username
		 FROM characters c
		 JOIN users u ON u.user_id = c.user_id
		 ORDER BY c.character_id DESC`,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายชื่อตัวละครไม่สำเร็จ")
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
		var ch adminCharView
		if err := rows.Scan(&ch.CharacterID, &ch.Name, &ch.Personality, &ch.IsShared, &ch.UsageCount, &ch.Rating, &ch.OwnerID, &ch.OwnerName); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายชื่อตัวละครไม่สำเร็จ")
		}
		characters = append(characters, ch)
	}

	return writeJSON(c, fiber.StatusOK, characters)
}

// ----- แอดมินลบตัวละครใดก็ได้ -----

func (h *AdminHandler) DeleteCharacter(c *fiber.Ctx) error {
	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	result, err := h.DB.Exec(`DELETE FROM characters WHERE character_id = $1`, characterID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ลบตัวละครไม่สำเร็จ")
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "ลบตัวละครสำเร็จ"})
}

// ----- ดูรายงานทั้งหมด -----

func (h *AdminHandler) ListReports(c *fiber.Ctx) error {
	rows, err := h.DB.Query(
		`SELECT r.report_id, r.details, r.status, r.character_id, ch.name, r.user_id, u.username, r.created_at
		 FROM reports r
		 JOIN characters ch ON ch.character_id = r.character_id
		 JOIN users u ON u.user_id = r.user_id
		 ORDER BY r.created_at DESC`,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายงานไม่สำเร็จ")
	}
	defer rows.Close()

	reports := []models.ReportAdminView{}
	for rows.Next() {
		var rp models.ReportAdminView
		if err := rows.Scan(&rp.ReportID, &rp.Details, &rp.Status, &rp.CharacterID, &rp.CharacterName, &rp.UserID, &rp.Username, &rp.CreatedAt); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายงานไม่สำเร็จ")
		}
		reports = append(reports, rp)
	}

	return writeJSON(c, fiber.StatusOK, reports)
}

// ----- ปิดเคสรายงาน -----

type updateReportStatusRequest struct {
	Status string `json:"status"` // "resolved" หรือ "rejected"
}

func (h *AdminHandler) UpdateReportStatus(c *fiber.Ctx) error {
	reportID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสรายงานไม่ถูกต้อง")
	}

	var req updateReportStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}
	if req.Status != "resolved" && req.Status != "rejected" {
		return writeError(c, fiber.StatusBadRequest, "สถานะต้องเป็น resolved หรือ rejected เท่านั้น")
	}

	result, err := h.DB.Exec(`UPDATE reports SET status = $1 WHERE report_id = $2`, req.Status, reportID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "อัปเดตสถานะไม่สำเร็จ")
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบรายงานนี้")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "อัปเดตสถานะสำเร็จ"})
}

// ----- สถิติภาพรวมระบบ -----

func (h *AdminHandler) Stats(c *fiber.Ctx) error {
	var totalUsers, totalCharacters, totalChats, pendingReports int

	h.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalUsers)
	h.DB.QueryRow(`SELECT COUNT(*) FROM characters`).Scan(&totalCharacters)
	h.DB.QueryRow(`SELECT COUNT(*) FROM chats`).Scan(&totalChats)
	h.DB.QueryRow(`SELECT COUNT(*) FROM reports WHERE status = 'pending'`).Scan(&pendingReports)

	return writeJSON(c, fiber.StatusOK, fiber.Map{
		"total_users":      totalUsers,
		"total_characters": totalCharacters,
		"total_chats":      totalChats,
		"pending_reports":  pendingReports,
	})
}
