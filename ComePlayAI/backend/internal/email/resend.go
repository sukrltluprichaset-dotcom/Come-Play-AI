package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ResendClient เป็นตัวเรียก Resend API (https://resend.com) สำหรับส่งอีเมลจริง
// ใช้แพทเทิร์นเดียวกับ GeminiClient ใน internal/llm/gemini.go (struct + http.Client + NewXxxClient)
type ResendClient struct {
	APIKey     string
	FromEmail  string
	HTTPClient *http.Client
}

func NewResendClient(apiKey, fromEmail string) *ResendClient {
	return &ResendClient{
		APIKey:     apiKey,
		FromEmail:  fromEmail,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type resendEmailRequest struct {
	From    string `json:"from"`
	To      []string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

// SendPasswordResetEmail ส่งอีเมลลิงก์รีเซ็ตรหัสผ่านไปยัง toEmail ผ่าน Resend API
// resetLink ควรเป็น URL เต็มที่ผู้ใช้กดแล้วไปหน้ารีเซ็ตรหัสผ่านได้เลย (เช่น https://.../?reset_token=xxxx)
func (c *ResendClient) SendPasswordResetEmail(toEmail, resetLink string) error {
	subject := "รีเซ็ตรหัสผ่านของคุณ - Come Play AI"
	html := fmt.Sprintf(`
		<div style="font-family: Arial, sans-serif; max-width: 480px; margin: 0 auto; padding: 24px;">
			<h2 style="color: #4f46e5;">รีเซ็ตรหัสผ่าน</h2>
			<p>เราได้รับคำขอให้รีเซ็ตรหัสผ่านของบัญชีนี้ กดปุ่มด้านล่างเพื่อตั้งรหัสผ่านใหม่ (ลิงก์นี้จะหมดอายุภายใน 1 ชั่วโมง)</p>
			<p style="text-align: center; margin: 32px 0;">
				<a href="%s" style="background: #4f46e5; color: #fff; padding: 12px 24px; border-radius: 8px; text-decoration: none; display: inline-block;">ตั้งรหัสผ่านใหม่</a>
			</p>
			<p style="color: #666; font-size: 13px;">หากคุณไม่ได้เป็นผู้ขอรีเซ็ตรหัสผ่าน สามารถเพิกเฉยต่ออีเมลนี้ได้ รหัสผ่านของคุณจะไม่ถูกเปลี่ยนแปลง</p>
			<p style="color: #999; font-size: 12px;">หากปุ่มด้านบนกดไม่ได้ ให้คัดลอกลิงก์นี้ไปวางในเบราว์เซอร์: %s</p>
		</div>`, resetLink, resetLink)

	reqBody := resendEmailRequest{
		From:    c.FromEmail,
		To:      []string{toEmail},
		Subject: subject,
		HTML:    html,
	}
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("แปลงข้อมูลอีเมลไม่สำเร็จ: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.resend.com/emails", bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("สร้าง request ส่งอีเมลไม่สำเร็จ: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("เรียก Resend API ไม่สำเร็จ: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("อ่านผลลัพธ์จาก Resend ไม่สำเร็จ: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("Resend API ตอบกลับ error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}
