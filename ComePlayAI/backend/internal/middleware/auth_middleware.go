package middleware

import (
	"context"
	"net/http"
	"strings"

	"comeplayai-backend/internal/auth"
)

type contextKey string

const (
	UserIDKey   contextKey = "userID"
	UserRoleKey contextKey = "userRole"
)

func RequireAuth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				writeUnauthorized(w)
				return
			}

			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := auth.ParseToken(tokenString, jwtSecret)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			// ฝากข้อมูล user ไว้ใน context ให้ handler ถัดไปดึงไปใช้ได้
			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, UserRoleKey, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin ใช้ต่อจาก RequireAuth เสมอ (ต้องรู้ตัวตนก่อนถึงจะเช็ค role ได้)
// เช็คว่า role ใน JWT token เป็น "admin" เท่านั้นถึงจะผ่าน
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, ok := r.Context().Value(UserRoleKey).(string)
		if !ok || role != "admin" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"ต้องเป็นผู้ดูแลระบบเท่านั้นถึงจะเข้าถึงส่วนนี้ได้"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"กรุณาเข้าสู่ระบบก่อนใช้งาน"}`))
}
