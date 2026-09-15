package handlers

import (
	"database/sql"
	"errors"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/lib/pq"

	"comeplayai-backend/internal/llm"
	"comeplayai-backend/internal/models"
)

const chatCost = 2 // จำนวนเหรียญที่หักต่อการส่งข้อความ 1 ครั้ง (ตรงกับผลทดสอบตารางที่ 4.3 ในเล่ม)

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

func (h *ChatHandler) SendMessage(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var req sendMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return writeError(c, fiber.StatusBadRequest, "ข้อความห้ามว่างเปล่า")
	}

	var isShared bool
	var ownerID int64
	var personality string
	err = h.DB.QueryRow(
		`SELECT is_shared, user_id, personality FROM characters WHERE character_id = $1`,
		characterID,
	).Scan(&isShared, &ownerID, &personality)

	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !isShared && ownerID != userID {
		return writeError(c, fiber.StatusForbidden, "ไม่มีสิทธิ์คุยกับตัวละครนี้")
	}

	var currentBalance int
	if err := h.DB.QueryRow(`SELECT balance FROM coins WHERE user_id = $1`, userID).Scan(&currentBalance); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if currentBalance < chatCost {
		return writeError(c, fiber.StatusPaymentRequired, "เหรียญไม่เพียงพอ กรุณาเติมเหรียญก่อนแชท")
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
		return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
	}
	var history []llm.ChatTurn
	for historyRows.Next() {
		var senderType, message string
		if err := historyRows.Scan(&senderType, &message); err != nil {
			historyRows.Close()
			return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
		}
		role := "user"
		if senderType == "ai" {
			role = "model"
		}
		history = append(history, llm.ChatTurn{Role: role, Text: message})
	}
	historyRows.Close()

	// เดิมเอาบุคลิก (personality) ที่ผู้ใช้พิมพ์ไว้ตอนสร้างตัวละครส่งให้ Gemini ตรง ๆ แบบไม่มีคำสั่งกำกับ
	// โทน/อารมณ์การพูดเลย พอผู้ใช้พิมพ์บุคลิกสั้น ๆ (เช่น "ร่าเริง สดใส") โมเดลเลยมักตอบแบบทางการ
	// ราบเรียบเหมือนแชทบอทบริการลูกค้า ไม่มีอารมณ์ความรู้สึกใด ๆ (ฟีดแบ็กผู้ใช้จริง: "เหมือนคุยกับหุ่นยนต์
	// ไม่มีอารมณ์อะไรตอนคุยเลย") เพิ่มคำสั่งสวมบทบาทกำกับไว้ก่อนบุคลิกของผู้ใช้เสมอ ให้ตอบแบบมีชีวิตชีวา
	// แสดงอารมณ์จริง ไม่ใช่พูดในมุมมอง AI ผู้ช่วย
	// อัปเดต: มีฟีดแบ็กเพิ่มว่าบางทีคำตอบยาวเกินไป อ่านเหมือนนิยายไม่เหมือนแชทคุยกัน เพิ่มกฎเรื่องความยาว
	// คำตอบให้ชัดเจนขึ้น (ปกติสั้น ๆ แบบข้อความแชทจริง ยาวขึ้นเฉพาะตอนจำเป็นจริง ๆ)
	const roleplayGuide = `คุณกำลังสวมบทบาทเป็นตัวละครในแอปแชท ไม่ใช่ผู้ช่วย AI ทั่วไป ให้ยึดบุคลิกที่กำหนดไว้ด้านล่างอย่างเคร่งครัด และทำตามกฎการพูดคุยต่อไปนี้เสมอ:
- ตอบในมุมมองบุคคลที่หนึ่งของตัวละคร (พูดแทนตัวเอง) ห้ามพูดถึงตัวเองว่าเป็น AI/โมเดลภาษา/ผู้ช่วย และห้ามใช้ประโยคทางการแบบบริการลูกค้า เช่น "ยินดีให้บริการค่ะ" หรือ "มีอะไรให้ช่วยเหลือเพิ่มเติมไหมคะ"
- แสดงอารมณ์ความรู้สึกออกมาให้เป็นธรรมชาติตามบุคลิกของตัวละคร (ดีใจ งอน หยอกล้อ ห่วงใย ตื่นเต้น ฯลฯ) ไม่ใช่ตอบแบบราบเรียบไร้ความรู้สึก
- ใช้ภาษาพูดที่เป็นธรรมชาติแบบคนคุยกันจริง ไม่ใช่ภาษาเขียนทางการ
- ตอบสั้นกระชับแบบข้อความแชทจริง ๆ ปกติแค่ 1-3 ประโยคต่อครั้งก็พอ ห้ามบรรยายฉาก ท่าทาง หรือใช้โวหารพรรณนายืดยาวแบบย่อหน้าในนิยาย เว้นแต่ผู้ใช้ถามคำถามที่ต้องอธิบายละเอียดจริง ๆ หรือขอให้เล่าเรื่องยาว ๆ เป็นพิเศษ ค่อยตอบยาวขึ้นเท่าที่จำเป็น
- ตอบสนองกับสิ่งที่ผู้ใช้พิมพ์มาจริง ๆ อย่างเข้าใจบริบทและอารมณ์ของผู้ใช้ ไม่ตอบเวียนซ้ำแบบสคริปต์ตายตัว

บุคลิกของตัวละครที่คุณต้องสวมบทบาท:
`

	// ----- RAG เฟส 2: ค้นหาความจำระยะยาวที่เกี่ยวข้อง (นอกเหนือจาก 20 ข้อความล่าสุด) -----
	fullPersonality := roleplayGuide + personality

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
			for _, cand := range candidates {
				score := cosineSimilarity(userEmbedding, cand.Embedding)
				if score > 0.75 {
					scoredList = append(scoredList, scored{Message: cand.Message, Score: score})
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
		// เดิมพอ Gemini ปฏิเสธเพราะตัวกรองความปลอดภัย (เช่นข้อความมีคำหยาบ) จะโชว์ข้อความ
		// "ระบบ AI ขัดข้อง กรุณาลองใหม่อีกครั้ง" เหมือนกับตอนระบบล่มจริง ๆ ทำให้ผู้ใช้เข้าใจผิดว่ากดลองใหม่
		// แล้วจะสำเร็จ ทั้งที่พิมพ์ข้อความเดิมซ้ำยังไงก็โดนบล็อกซ้ำแน่นอน แยกเคสนี้ออกมาบอกสาเหตุจริงแทน
		if errors.Is(err, llm.ErrContentBlocked) {
			return writeError(c, fiber.StatusBadRequest, "ข้อความนี้มีเนื้อหาที่ไม่เหมาะสม ระบบ AI ไม่สามารถตอบกลับได้ กรุณาลองพิมพ์ข้อความอื่น")
		}
		return writeError(c, fiber.StatusInternalServerError, "ระบบ AI ขัดข้อง กรุณาลองใหม่อีกครั้ง")
	}

	aiEmbedding, embedErr := h.Gemini.EmbedText(aiReply)
	if embedErr != nil {
		log.Printf("Embedding error (คำตอบ AI): %v", embedErr)
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
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
		return writeError(c, fiber.StatusInternalServerError, "ส่งข้อความไม่สำเร็จ")
	}

	var aiChat models.Chat
	err = tx.QueryRow(
		`INSERT INTO chats (sender_type, message, user_id, character_id, embedding)
		 VALUES ('ai', $1, $2, $3, $4)
		 RETURNING chat_id, sender_type, message, send_time, user_id, character_id`,
		aiReply, userID, characterID, pq.Array(aiEmbedding),
	).Scan(&aiChat.ChatID, &aiChat.SenderType, &aiChat.Message, &aiChat.SendTime, &aiChat.UserID, &aiChat.CharacterID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "รับคำตอบไม่สำเร็จ")
	}

	if _, err := tx.Exec(`UPDATE characters SET usage_count = usage_count + 1 WHERE character_id = $1`, characterID); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "อัปเดตสถิติไม่สำเร็จ")
	}

	var newBalance int
	err = tx.QueryRow(
		`UPDATE coins SET balance = balance - $1 WHERE user_id = $2 AND balance >= $1 RETURNING balance`,
		chatCost, userID,
	).Scan(&newBalance)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusPaymentRequired, "เหรียญไม่เพียงพอ กรุณาเติมเหรียญก่อนแชท")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "หักเหรียญไม่สำเร็จ")
	}

	if err := tx.Commit(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ส่งข้อความไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusCreated, fiber.Map{
		"user_message": userChat,
		"ai_message":   aiChat,
		"coin_spent":   chatCost,
		"coin_balance": newBalance,
	})
}

func (h *ChatHandler) GetHistory(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	rows, err := h.DB.Query(
		`SELECT chat_id, sender_type, message, send_time, user_id, character_id
		 FROM chats WHERE user_id = $1 AND character_id = $2 ORDER BY send_time ASC`,
		userID, characterID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
	}
	defer rows.Close()

	chats := []models.Chat{}
	for rows.Next() {
		var ch models.Chat
		if err := rows.Scan(&ch.ChatID, &ch.SenderType, &ch.Message, &ch.SendTime, &ch.UserID, &ch.CharacterID); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
		}
		chats = append(chats, ch)
	}

	return writeJSON(c, fiber.StatusOK, chats)
}
