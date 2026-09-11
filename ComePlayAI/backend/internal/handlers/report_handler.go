package handlers

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/models"
)

type ReportHandler struct {
	DB *sql.DB
}

func NewReportHandler(db *sql.DB) *ReportHandler {
	return &ReportHandler{DB: db}
}

type reportRequest struct {
	Details string `json:"details"`
}

func (h *ReportHandler) Create(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var req reportRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Details = strings.TrimSpace(req.Details)
	if req.Details == "" {
		return writeError(c, fiber.StatusBadRequest, "กรุณาระบุรายละเอียดการรายงาน")
	}

	var exists bool
	if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM characters WHERE character_id = $1)`, characterID).Scan(&exists); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !exists {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	}

	var report models.Report
	err = h.DB.QueryRow(
		`INSERT INTO reports (details, user_id, character_id)
		 VALUES ($1, $2, $3)
		 RETURNING report_id, details, status, user_id, character_id, created_at`,
		req.Details, userID, characterID,
	).Scan(&report.ReportID, &report.Details, &report.Status, &report.UserID, &report.CharacterID, &report.CreatedAt)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ส่งรายงานไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusCreated, report)
}
