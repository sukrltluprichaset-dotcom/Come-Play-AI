package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"comeplayai-backend/internal/models"
)

type EvaluationHandler struct {
	DB *sql.DB
}

func NewEvaluationHandler(db *sql.DB) *EvaluationHandler {
	return &EvaluationHandler{DB: db}
}

type evaluationAnswer struct {
	Question string `json:"question"`
	Answer   int    `json:"answer"`
}

type submitEvaluationRequest struct {
	Answers []evaluationAnswer `json:"answers"`
}

// Submit บันทึกคำตอบแบบประเมินหลายข้อพร้อมกันในครั้งเดียว
func (h *EvaluationHandler) Submit(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	var req submitEvaluationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}

	if len(req.Answers) == 0 {
		writeError(w, http.StatusBadRequest, "กรุณาตอบแบบประเมินให้ครบถ้วน")
		return
	}
	for _, a := range req.Answers {
		if strings.TrimSpace(a.Question) == "" {
			writeError(w, http.StatusBadRequest, "กรุณากรอกแบบประเมินให้ครบถ้วน")
			return
		}
		if a.Answer < 1 || a.Answer > 5 {
			writeError(w, http.StatusBadRequest, "คะแนนต้องอยู่ระหว่าง 1-5")
			return
		}
	}

	tx, err := h.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	defer tx.Rollback()

	saved := []models.Evaluation{}
	for _, a := range req.Answers {
		var ev models.Evaluation
		err = tx.QueryRow(
			`INSERT INTO evaluations (question, answer, user_id)
			 VALUES ($1, $2, $3)
			 RETURNING eval_id, question, answer, user_id, created_at`,
			strings.TrimSpace(a.Question), a.Answer, userID,
		).Scan(&ev.EvalID, &ev.Question, &ev.Answer, &ev.UserID, &ev.CreatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "บันทึกแบบประเมินไม่สำเร็จ")
			return
		}
		saved = append(saved, ev)
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "บันทึกแบบประเมินไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusCreated, saved)
}
