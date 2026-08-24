package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxUploadSize = 20 << 20 // 20 MB
	uploadDir     = "uploads"
)

type UploadHandler struct{}

func NewUploadHandler() *UploadHandler {
	os.MkdirAll(uploadDir, 0755)
	return &UploadHandler{}
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

	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	dstPath := filepath.Join(uploadDir, filename)

	dst, err := os.Create(dstPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "บันทึกไฟล์ไม่สำเร็จ")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		writeError(w, http.StatusInternalServerError, "บันทึกไฟล์ไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": "/uploads/" + filename})
}
