package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const maxUploadSize = 20 << 20 // 20 MB

type UploadHandler struct {
	SupabaseURL        string
	SupabaseServiceKey string
	SupabaseBucket     string
	HTTPClient         *http.Client
}

func NewUploadHandler(supabaseURL, serviceKey, bucket string) *UploadHandler {
	return &UploadHandler{
		SupabaseURL:        supabaseURL,
		SupabaseServiceKey: serviceKey,
		SupabaseBucket:     bucket,
		HTTPClient:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *UploadHandler) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "ไฟล์มีขนาดใหญ่เกินไป (สูงสุด 20MB)")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "ไม่พบไฟล์ที่ส่งมา")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExt := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		".mp4": true, ".webm": true,
	}
	if !allowedExt[ext] {
		writeError(w, http.StatusBadRequest, "รองรับเฉพาะไฟล์ภาพ (jpg, png, gif, webp) หรือวิดีโอ (mp4, webm)")
		return
	}

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "อ่านไฟล์ไม่สำเร็จ")
		return
	}

	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)

	// อัปโหลดไปยัง Supabase Storage ผ่าน REST API โดยตรง
	uploadURL := fmt.Sprintf("%s/storage/v1/object/%s/%s", h.SupabaseURL, h.SupabaseBucket, filename)

	req, err := http.NewRequest(http.MethodPost, uploadURL, bytes.NewReader(fileBytes))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "สร้าง request ไม่สำเร็จ")
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.SupabaseServiceKey)
	req.Header.Set("Content-Type", header.Header.Get("Content-Type"))

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "อัปโหลดไฟล์ไม่สำเร็จ")
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("อัปโหลดไฟล์ไม่สำเร็จ (status %d): %s", resp.StatusCode, string(respBody)))
		return
	}

	publicURL := fmt.Sprintf("%s/storage/v1/object/public/%s/%s", h.SupabaseURL, h.SupabaseBucket, filename)

	writeJSON(w, http.StatusOK, map[string]string{"url": publicURL})
}
