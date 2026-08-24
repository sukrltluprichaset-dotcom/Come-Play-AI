package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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

func (h *ReportHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	characterID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
		return
	}

	var req reportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}

	req.Details = strings.TrimSpace(req.Details)
	if req.Details == "" {
		writeError(w, http.StatusBadRequest, "กรุณาระบุรายละเอียดการรายงาน")
		return
	}

	var exists bool
	if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM characters WHERE character_id = $1)`, characterID).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "ไม่พบตัวละครนี้")
		return
	}

	var report models.Report
	err = h.DB.QueryRow(
		`INSERT INTO reports (details, user_id, character_id)
		 VALUES ($1, $2, $3)
		 RETURNING report_id, details, status, user_id, character_id, created_at`,
		req.Details, userID, characterID,
	).Scan(&report.ReportID, &report.Details, &report.Status, &report.UserID, &report.CharacterID, &report.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ส่งรายงานไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusCreated, report)
}
