package middleware

import (
	"context"
	"net/http"
	"strings"

	"comeplayai-backend/internal/auth"
)

type contextKey string

const UserIDKey contextKey = "userID"
const UserRoleKey contextKey = "userRole"

// RequireAuth คืนค่า middleware ที่เช็ค Authorization header ก่อนอนุญาตให้เข้า handler ถัดไป
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

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"กรุณาเข้าสู่ระบบก่อนใช้งาน"}`))
}
