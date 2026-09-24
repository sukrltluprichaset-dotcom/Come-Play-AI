package handlers

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/llm"
)

// จำกัดมินิเกม "ตอบคำถามเกี่ยวกับตัวละคร" ไว้วันละ 2 รอบต่อบัญชี (รวมทุกตัวละคร ไม่ใช่แยกนับต่อตัวละคร)
// และให้เหรียญรางวัลตามจำนวนข้อที่ตอบถูก ข้อละ quizRewardPerCorrect เหรียญ (เต็ม 5 ข้อ = ได้สูงสุด 25 เหรียญ/รอบ)
const (
	quizMaxAttemptsPerDay = 2
	quizRewardPerCorrect  = 5
)

type QuizHandler struct {
	DB     *sql.DB
	Gemini *llm.GeminiClient
}

func NewQuizHandler(db *sql.DB, gemini *llm.GeminiClient) *QuizHandler {
	return &QuizHandler{DB: db, Gemini: gemini}
}

// quizQuestionInternal เก็บเฉลย (CorrectIndex) ไว้ใช้ภายในฝั่ง backend เท่านั้น
type quizQuestionInternal struct {
	Question     string   `json:"question"`
	Choices      []string `json:"choices"`
	CorrectIndex int      `json:"correct_index"`
}

// quizQuestionPublic คือรูปแบบที่ส่งให้ frontend ตอนเริ่มเกม (ไม่มีเฉลยติดไปด้วย กันโกงผ่าน Network tab)
type quizQuestionPublic struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
}

// countTodayAttempts นับจำนวนรอบที่ผู้ใช้คนนี้เริ่มเล่นมินิเกมนี้ไปแล้ว "วันนี้" (นับรวมทุกตัวละคร
// เพราะโควตานี้เป็นโควตาต่อบัญชีต่อวัน ไม่ใช่แยกโควตาต่อตัวละคร)
func (h *QuizHandler) countTodayAttempts(userID int64) (int, error) {
	var count int
	err := h.DB.QueryRow(
		`SELECT COUNT(*) FROM quiz_attempts WHERE user_id = $1 AND started_at::date = CURRENT_DATE`,
		userID,
	).Scan(&count)
	return count, err
}

// Status บอกจำนวนรอบที่เล่นไปแล้ว/เหลือของวันนี้ ให้ frontend เอาไปโชว์ข้อความหรือปิดปุ่ม "เริ่มเล่น"
// ก่อนที่ผู้ใช้จะกดเริ่มเกมจริง (กันเสีย quota ฟรีๆ กับ error message ตอนกดไปแล้วเจอว่าครบโควตา)
func (h *QuizHandler) Status(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	used, err := h.countTodayAttempts(userID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ตรวจสอบสถานะมินิเกมไม่สำเร็จ")
	}
	left := quizMaxAttemptsPerDay - used
	if left < 0 {
		left = 0
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{
		"attempts_used_today": used,
		"attempts_left_today": left,
		"max_per_day":         quizMaxAttemptsPerDay,
	})
}

// Start เริ่มมินิเกม 1 รอบ: เช็คสิทธิ์เข้าถึงตัวละคร + โควตารายวันก่อน แล้วให้ Gemini แต่งคำถามปรนัย
// 5 ข้อเกี่ยวกับตัวละครนี้ บันทึกคำถาม+เฉลยไว้ในฐานข้อมูล แต่ส่งกลับไปให้ frontend เฉพาะคำถาม+ตัวเลือก
func (h *QuizHandler) Start(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var characterName, personality string
	var isShared bool
	var ownerID int64
	err = h.DB.QueryRow(
		`SELECT name, personality, is_shared, user_id FROM characters WHERE character_id = $1`,
		characterID,
	).Scan(&characterName, &personality, &isShared, &ownerID)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !isShared && ownerID != userID {
		return writeError(c, fiber.StatusForbidden, "ไม่มีสิทธิ์เข้าถึงตัวละครนี้")
	}

	used, err := h.countTodayAttempts(userID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ตรวจสอบโควตามินิเกมไม่สำเร็จ")
	}
	if used >= quizMaxAttemptsPerDay {
		return writeError(c, fiber.StatusTooManyRequests, "วันนี้เล่นมินิเกมครบ 2 รอบแล้ว พรุ่งนี้ค่อยมาเล่นใหม่นะครับ")
	}

	raw, err := h.Gemini.GenerateQuiz(characterName, personality)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "สร้างคำถามไม่สำเร็จ กรุณาลองใหม่อีกครั้ง")
	}

	// กันเผื่อ Gemini แถม ```json ... ``` ครอบมาทั้งที่สั่งห้ามไปแล้วในพรอมต์
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var questions []quizQuestionInternal
	if err := json.Unmarshal([]byte(cleaned), &questions); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "สร้างคำถามไม่สำเร็จ กรุณาลองใหม่อีกครั้ง")
	}
	if len(questions) != 5 {
		return writeError(c, fiber.StatusInternalServerError, "สร้างคำถามไม่สำเร็จ กรุณาลองใหม่อีกครั้ง")
	}
	for _, q := range questions {
		if strings.TrimSpace(q.Question) == "" || len(q.Choices) != 4 || q.CorrectIndex < 0 || q.CorrectIndex > 3 {
			return writeError(c, fiber.StatusInternalServerError, "สร้างคำถามไม่สำเร็จ กรุณาลองใหม่อีกครั้ง")
		}
	}

	questionsJSON, err := json.Marshal(questions)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	var attemptID int64
	err = h.DB.QueryRow(
		`INSERT INTO quiz_attempts (user_id, character_id, questions, started_at)
		 VALUES ($1, $2, $3, NOW()) RETURNING attempt_id`,
		userID, characterID, questionsJSON,
	).Scan(&attemptID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เริ่มมินิเกมไม่สำเร็จ")
	}

	publicQuestions := make([]quizQuestionPublic, len(questions))
	for i, q := range questions {
		publicQuestions[i] = quizQuestionPublic{Question: q.Question, Choices: q.Choices}
	}

	return writeJSON(c, fiber.StatusCreated, fiber.Map{
		"attempt_id":          attemptID,
		"character_name":      characterName,
		"questions":           publicQuestions,
		"attempts_left_today": quizMaxAttemptsPerDay - used - 1,
	})
}

type submitQuizRequest struct {
	Answers []int `json:"answers"`
}

// Submit ตรวจคำตอบของรอบที่เริ่มไปแล้ว คำนวณคะแนน+เหรียญรางวัล แล้วเติมเหรียญเข้าบัญชีทันที
// (กันส่งซ้ำ/โกงด้วยการเช็คว่ารอบนี้เป็นของ user นี้จริง และยังไม่เคยส่งคำตอบมาก่อน)
func (h *QuizHandler) Submit(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	attemptID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสรอบเล่นไม่ถูกต้อง")
	}

	var req submitQuizRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	var attemptUserID int64
	var questionsJSON []byte
	var submittedAt sql.NullTime
	err = h.DB.QueryRow(
		`SELECT user_id, questions, submitted_at FROM quiz_attempts WHERE attempt_id = $1`,
		attemptID,
	).Scan(&attemptUserID, &questionsJSON, &submittedAt)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบรอบเล่นนี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if attemptUserID != userID {
		return writeError(c, fiber.StatusForbidden, "ไม่มีสิทธิ์เข้าถึงรอบเล่นนี้")
	}
	if submittedAt.Valid {
		return writeError(c, fiber.StatusConflict, "รอบเล่นนี้ส่งคำตอบไปแล้ว")
	}

	var questions []quizQuestionInternal
	if err := json.Unmarshal(questionsJSON, &questions); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if len(req.Answers) != len(questions) {
		return writeError(c, fiber.StatusBadRequest, "จำนวนคำตอบไม่ตรงกับจำนวนคำถาม")
	}

	score := 0
	correctIndexes := make([]int, len(questions))
	for i, q := range questions {
		correctIndexes[i] = q.CorrectIndex
		if req.Answers[i] == q.CorrectIndex {
			score++
		}
	}
	rewardCoin := score * quizRewardPerCorrect

	answersJSON, err := json.Marshal(req.Answers)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE quiz_attempts SET answers = $1, score = $2, reward_coin = $3, submitted_at = NOW() WHERE attempt_id = $4`,
		answersJSON, score, rewardCoin, attemptID,
	); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "บันทึกผลไม่สำเร็จ")
	}

	var newBalance int
	err = tx.QueryRow(
		`UPDATE coins SET balance = balance + $1 WHERE user_id = $2 RETURNING balance`,
		rewardCoin, userID,
	).Scan(&newBalance)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เติมเหรียญไม่สำเร็จ")
	}

	if err := tx.Commit(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "บันทึกผลไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{
		"score":           score,
		"total_questions": len(questions),
		"reward_coin":     rewardCoin,
		"correct_indexes": correctIndexes,
		"new_balance":     newBalance,
	})
}
