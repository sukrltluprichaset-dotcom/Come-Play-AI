package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type GeminiClient struct {
	APIKey         string
	Model          string
	FallbackModels []string
	HTTPClient     *http.Client
}

func NewGeminiClient(apiKey string) *GeminiClient {
	return &GeminiClient{
		APIKey: apiKey,
		Model:  "gemini-flash-latest",
		// โมเดลสำรอง ใช้เมื่อโมเดลหลักโหลดสูง (503) หรือติด rate limit (429)
		// ใช้ชื่อแบบ -latest ทั้งหมด กัน Google ปลดระวางรุ่นแล้วโค้ดพัง
		FallbackModels: []string{"gemini-flash-lite-latest", "gemini-pro-latest"},
		HTTPClient:     &http.Client{Timeout: 60 * time.Second},
	}
}

type ChatTurn struct {
	Role string
	Text string
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiSystemInstruction struct {
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	Contents          []geminiContent          `json:"contents"`
	SystemInstruction *geminiSystemInstruction `json:"systemInstruction,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// callModel ยิง request ไปยังโมเดลที่ระบุ 1 ครั้ง คืนค่าคำตอบ, สถานะ "ควรลองโมเดลอื่นไหม", และ error
func (c *GeminiClient) callModel(model string, jsonBody []byte) (string, bool, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", false, fmt.Errorf("สร้าง request ไม่สำเร็จ: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", true, fmt.Errorf("เรียก Gemini API (%s) ไม่สำเร็จ: %w", model, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", true, fmt.Errorf("อ่านผลลัพธ์ไม่สำเร็จ: %w", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests {
		return "", true, fmt.Errorf("โมเดล %s ตอบกลับ error (status %d): %s", model, resp.StatusCode, string(bodyBytes))
	}

	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("Gemini API (%s) ตอบกลับ error (status %d): %s", model, resp.StatusCode, string(bodyBytes))
	}

	var result geminiResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return "", false, fmt.Errorf("แปลงผลลัพธ์ไม่สำเร็จ: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", true, fmt.Errorf("ไม่ได้รับคำตอบจากโมเดล %s", model)
	}

	return result.Candidates[0].Content.Parts[0].Text, false, nil
}

// GenerateReply ส่งบุคลิกตัวละคร + ประวัติการสนทนา + ข้อความใหม่ ไปให้ Gemini
// ถ้าโมเดลหลักเจอ error ชั่วคราว (503/429) จะลองซ้ำ 2 ครั้ง แล้วสลับไปโมเดลสำรองตัวถัดไปอัตโนมัติ
func (c *GeminiClient) GenerateReply(personality string, history []ChatTurn, userMessage string) (string, error) {
	contents := make([]geminiContent, 0, len(history)+1)
	for _, turn := range history {
		contents = append(contents, geminiContent{
			Role:  turn.Role,
			Parts: []geminiPart{{Text: turn.Text}},
		})
	}
	contents = append(contents, geminiContent{
		Role:  "user",
		Parts: []geminiPart{{Text: userMessage}},
	})

	reqBody := geminiRequest{
		Contents: contents,
		SystemInstruction: &geminiSystemInstruction{
			Parts: []geminiPart{{Text: personality}},
		},
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("แปลงข้อมูลไม่สำเร็จ: %w", err)
	}

	modelsToTry := append([]string{c.Model}, c.FallbackModels...)
	const retriesPerModel = 2
	var lastErr error

	for _, model := range modelsToTry {
		for attempt := 1; attempt <= retriesPerModel; attempt++ {
			reply, retryable, err := c.callModel(model, jsonBody)
			if err == nil {
				return reply, nil
			}
			lastErr = err
			if !retryable {
				return "", lastErr // error ถาวร (ไม่ใช่โหลดสูง) ไม่ต้องลองโมเดลอื่นต่อ
			}
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}

	return "", fmt.Errorf("ลองทุกโมเดลแล้วยังไม่สำเร็จ: %w", lastErr)
}

type embedRequestContent struct {
	Parts []geminiPart `json:"parts"`
}

// SummarizeDiary ให้ AI สรุปบทสนทนาของวันนี้เป็นบันทึกประจำวันในมุมมองของตัวละครเอง
func (c *GeminiClient) SummarizeDiary(characterName, personality, conversationText string) (string, error) {
	diaryPrompt := fmt.Sprintf(
		`คุณคือ "%s" ผู้มีบุคลิกภาพดังนี้: %s

หน้าที่ของคุณตอนนี้คือเขียนบันทึกประจำวัน (ไดอารี่) สรุปบทสนทนาที่เกิดขึ้นกับผู้ใช้วันนี้ โดยเขียนในมุมมองบุคคลที่หนึ่งของคุณเอง (เหมือนกำลังจดบันทึกความทรงจำ) สั้นกระชับ 3-5 ประโยค เน้นเรื่องสำคัญหรืออารมณ์ความรู้สึกที่เกิดขึ้น ไม่ต้องทักทายหรือปิดท้าย เขียนเป็นเนื้อหาบันทึกล้วนๆ`,
		characterName, personality,
	)

	return c.GenerateReply(diaryPrompt, nil, conversationText)
}

type embedRequest struct {
	Content              embedRequestContent `json:"content"`
	OutputDimensionality int                 `json:"outputDimensionality"`
}

type embedResponse struct {
	Embedding struct {
		Values []float64 `json:"values"`
	} `json:"embedding"`
}

// EmbedText แปลงข้อความเป็นเวกเตอร์ (768 มิติ) ใช้สำหรับค้นหาความคล้ายคลึงเชิงความหมาย
func (c *GeminiClient) EmbedText(text string) ([]float64, error) {
	reqBody := embedRequest{
		Content:              embedRequestContent{Parts: []geminiPart{{Text: text}}},
		OutputDimensionality: 768,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("แปลงข้อมูลไม่สำเร็จ: %w", err)
	}

	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:embedContent"

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("สร้าง request ไม่สำเร็จ: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("เรียก Embedding API ไม่สำเร็จ: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("อ่านผลลัพธ์ไม่สำเร็จ: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Embedding API ตอบกลับ error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result embedResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("แปลงผลลัพธ์ไม่สำเร็จ: %w", err)
	}

	return result.Embedding.Values, nil
}
