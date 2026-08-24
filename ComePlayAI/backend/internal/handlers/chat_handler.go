package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/lib/pq"

	"comeplayai-backend/internal/llm"
	"comeplayai-backend/internal/models"
)

const chatCost = 1 // จำนวนเหรียญที่หักต่อการส่งข้อความ 1 ครั้ง

type ChatHandler struct {
	DB     *sql.DB
	Gemini *llm.GeminiClient
}

func NewChatHandler(db *sql.DB, gemini *llm.GeminiClient) *ChatHandler {
	return &ChatHandler{DB: db, Gemini: gemini}
}

type sendMessageRequest struct {
	Message string `json:"message"`
}

// cosineSimilarity คำนวณความคล้ายคลึงเชิงความหมายระหว่างเวกเตอร์ 2 ตัว (ค่ายิ่งใกล้ 1 ยิ่งคล้ายกันมาก)
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func (h *ChatHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	characterID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
		return
	}

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "ข้อความห้ามว่างเปล่า")
		return
	}

	var isShared bool
	var ownerID int64
	var personality string
	err = h.DB.QueryRow(
		`SELECT is_shared, user_id, personality FROM characters WHERE character_id = $1`,
		characterID,
	).Scan(&isShared, &ownerID, &personality)

	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "ไม่พบตัวละครนี้")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	if !isShared && ownerID != userID {
		writeError(w, http.StatusForbidden, "ไม่มีสิทธิ์คุยกับตัวละครนี้")
		return
	}

	var currentBalance int
	if err := h.DB.QueryRow(`SELECT balance FROM coins WHERE user_id = $1`, userID).Scan(&currentBalance); err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	if currentBalance < chatCost {
		writeError(w, http.StatusPaymentRequired, "เหรียญไม่เพียงพอ กรุณาเติมเหรียญก่อนแชท")
		return
	}

	// ----- ดึงความจำระยะสั้น: 20 ข้อความล่าสุดในห้องนี้ -----
	historyRows, err := h.DB.Query(
		`SELECT sender_type, message FROM (
			SELECT sender_type, message, send_time FROM chats
			WHERE user_id = $1 AND character_id = $2
			ORDER BY send_time DESC LIMIT 20
		) recent ORDER BY send_time ASC`,
		userID, characterID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
		return
	}
	var history []llm.ChatTurn
	for historyRows.Next() {
		var senderType, message string
		if err := historyRows.Scan(&senderType, &message); err != nil {
			historyRows.Close()
			writeError(w, http.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
			return
		}
		role := "user"
		if senderType == "ai" {
			role = "model"
		}
		history = append(history, llm.ChatTurn{Role: role, Text: message})
	}
	historyRows.Close()

	// ----- RAG เฟส 2: ค้นหาความจำระยะยาวที่เกี่ยวข้อง (นอกเหนือจาก 20 ข้อความล่าสุด) -----
	fullPersonality := personality

	userEmbedding, embedErr := h.Gemini.EmbedText(req.Message)
	if embedErr != nil {
		log.Printf("Embedding error (ข้ามการค้นหาความจำระยะยาวรอบนี้): %v", embedErr)
	} else {
		type oldMsg struct {
			Message   string
			Embedding []float64
		}
		oldRows, err := h.DB.Query(
			`SELECT message, embedding FROM chats
			 WHERE user_id = $1 AND character_id = $2 AND embedding IS NOT NULL
			 ORDER BY send_time DESC OFFSET 20 LIMIT 300`,
			userID, characterID,
		)
		if err == nil {
			var candidates []oldMsg
			for oldRows.Next() {
				var m oldMsg
				if err := oldRows.Scan(&m.Message, pq.Array(&m.Embedding)); err == nil {
					candidates = append(candidates, m)
				}
			}
			oldRows.Close()

			type scored struct {
				Message string
				Score   float64
			}
			var scoredList []scored
			for _, c := range candidates {
				score := cosineSimilarity(userEmbedding, c.Embedding)
				if score > 0.75 {
					scoredList = append(scoredList, scored{Message: c.Message, Score: score})
				}
			}
			sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].Score > scoredList[j].Score })

			if len(scoredList) > 0 {
				limit := 5
				if len(scoredList) < limit {
					limit = len(scoredList)
				}
				var sb strings.Builder
				sb.WriteString("\n\n[ความทรงจำเก่าที่เกี่ยวข้องกับสิ่งที่ผู้ใช้เพิ่งพูดถึง]\n")
				for _, s := range scoredList[:limit] {
					sb.WriteString("- " + s.Message + "\n")
				}
				fullPersonality += sb.String()
			}
		}
	}

	aiReply, err := h.Gemini.GenerateReply(fullPersonality, history, req.Message)
	if err != nil {
		log.Printf("Gemini API error: %v", err)
		writeError(w, http.StatusInternalServerError, "ระบบ AI ขัดข้อง กรุณาลองใหม่อีกครั้ง")
		return
	}

	aiEmbedding, embedErr := h.Gemini.EmbedText(aiReply)
	if embedErr != nil {
		log.Printf("Embedding error (คำตอบ AI): %v", embedErr)
	}

	tx, err := h.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	defer tx.Rollback()

	var userChat models.Chat
	err = tx.QueryRow(
		`INSERT INTO chats (sender_type, message, user_id, character_id, embedding)
		 VALUES ('user', $1, $2, $3, $4)
		 RETURNING chat_id, sender_type, message, send_time, user_id, character_id`,
		req.Message, userID, characterID, pq.Array(userEmbedding),
	).Scan(&userChat.ChatID, &userChat.SenderType, &userChat.Message, &userChat.SendTime, &userChat.UserID, &userChat.CharacterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ส่งข้อความไม่สำเร็จ")
		return
	}

	var aiChat models.Chat
	err = tx.QueryRow(
		`INSERT INTO chats (sender_type, message, user_id, character_id, embedding)
		 VALUES ('ai', $1, $2, $3, $4)
		 RETURNING chat_id, sender_type, message, send_time, user_id, character_id`,
		aiReply, userID, characterID, pq.Array(aiEmbedding),
	).Scan(&aiChat.ChatID, &aiChat.SenderType, &aiChat.Message, &aiChat.SendTime, &aiChat.UserID, &aiChat.CharacterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "รับคำตอบไม่สำเร็จ")
		return
	}

	if _, err := tx.Exec(`UPDATE characters SET usage_count = usage_count + 1 WHERE character_id = $1`, characterID); err != nil {
		writeError(w, http.StatusInternalServerError, "อัปเดตสถิติไม่สำเร็จ")
		return
	}

	var newBalance int
	err = tx.QueryRow(
		`UPDATE coins SET balance = balance - $1 WHERE user_id = $2 AND balance >= $1 RETURNING balance`,
		chatCost, userID,
	).Scan(&newBalance)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusPaymentRequired, "เหรียญไม่เพียงพอ กรุณาเติมเหรียญก่อนแชท")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "หักเหรียญไม่สำเร็จ")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "ส่งข้อความไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"user_message": userChat,
		"ai_message":   aiChat,
		"coin_spent":   chatCost,
		"coin_balance": newBalance,
	})
}

func (h *ChatHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	characterID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
		return
	}

	rows, err := h.DB.Query(
		`SELECT chat_id, sender_type, message, send_time, user_id, character_id
		 FROM chats WHERE user_id = $1 AND character_id = $2 ORDER BY send_time ASC`,
		userID, characterID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
		return
	}
	defer rows.Close()

	chats := []models.Chat{}
	for rows.Next() {
		var c models.Chat
		if err := rows.Scan(&c.ChatID, &c.SenderType, &c.Message, &c.SendTime, &c.UserID, &c.CharacterID); err != nil {
			writeError(w, http.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
			return
		}
		chats = append(chats, c)
	}

	writeJSON(w, http.StatusOK, chats)
}
