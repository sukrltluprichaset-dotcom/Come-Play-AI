package main

import (
	"encoding/json"
	"log"
	"net/http"

	"comeplayai-backend/internal/config"
	"comeplayai-backend/internal/database"
	"comeplayai-backend/internal/handlers"
	"comeplayai-backend/internal/llm"
	"comeplayai-backend/internal/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("โหลด config ไม่สำเร็จ: %v", err)
	}

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("เชื่อมต่อฐานข้อมูลไม่สำเร็จ: %v", err)
	}
	defer db.Close()

	log.Printf("เชื่อมต่อฐานข้อมูล %q สำเร็จ\n", cfg.DBName)

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		count, err := database.PackageCount(db)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"status":          "error",
				"database_status": "disconnected",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"database_status": "connected",
			"package_count":   count,
		})
	})

	authMW := middleware.RequireAuth(cfg.JWTSecret)

	authHandler := handlers.NewAuthHandler(db, cfg.JWTSecret)
	mux.HandleFunc("POST /api/auth/register", authHandler.Register)
	mux.HandleFunc("POST /api/auth/login", authHandler.Login)
	mux.Handle("PUT /api/auth/password", authMW(http.HandlerFunc(authHandler.ChangePassword)))

	characterHandler := handlers.NewCharacterHandler(db)
	mux.Handle("POST /api/characters", authMW(http.HandlerFunc(characterHandler.Create)))
	mux.Handle("GET /api/characters", authMW(http.HandlerFunc(characterHandler.ListMine)))
	mux.HandleFunc("GET /api/characters/public", characterHandler.ListPublic)
	mux.HandleFunc("GET /api/characters/popular", characterHandler.Popular)
	mux.Handle("GET /api/characters/stats", authMW(http.HandlerFunc(characterHandler.Stats)))
	mux.Handle("GET /api/characters/{id}", authMW(http.HandlerFunc(characterHandler.Get)))
	mux.Handle("PUT /api/characters/{id}", authMW(http.HandlerFunc(characterHandler.Update)))
	mux.Handle("DELETE /api/characters/{id}", authMW(http.HandlerFunc(characterHandler.Delete)))

	reviewHandler := handlers.NewReviewHandler(db)
	mux.Handle("POST /api/characters/{id}/reviews", authMW(http.HandlerFunc(reviewHandler.CreateOrUpdate)))
	mux.HandleFunc("GET /api/characters/{id}/reviews", reviewHandler.List)
	reportHandler := handlers.NewReportHandler(db)
	mux.Handle("POST /api/characters/{id}/reports", authMW(http.HandlerFunc(reportHandler.Create)))
	evaluationHandler := handlers.NewEvaluationHandler(db)
	mux.Handle("POST /api/evaluations", authMW(http.HandlerFunc(evaluationHandler.Submit)))
	uploadHandler := handlers.NewUploadHandler()
	mux.Handle("POST /api/uploads", authMW(http.HandlerFunc(uploadHandler.Upload)))
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	geminiClient := llm.NewGeminiClient(cfg.GeminiAPIKey)
	chatHandler := handlers.NewChatHandler(db, geminiClient)
	mux.Handle("POST /api/characters/{id}/chats", authMW(http.HandlerFunc(chatHandler.SendMessage)))
	mux.Handle("GET /api/characters/{id}/chats", authMW(http.HandlerFunc(chatHandler.GetHistory)))

	coinHandler := handlers.NewCoinHandler(db)
	mux.Handle("GET /api/coins", authMW(http.HandlerFunc(coinHandler.GetBalance)))

	activityHandler := handlers.NewActivityHandler(db)
	mux.HandleFunc("GET /api/activities", activityHandler.List)
	mux.Handle("POST /api/activities/{id}/claim", authMW(http.HandlerFunc(activityHandler.Claim)))

	diaryHandler := handlers.NewDiaryHandler(db, geminiClient)
	mux.Handle("POST /api/characters/{id}/diary", authMW(http.HandlerFunc(diaryHandler.Generate)))
	mux.Handle("GET /api/diaries", authMW(http.HandlerFunc(diaryHandler.List)))

	adminHandler := handlers.NewAdminHandler(db)
	mux.Handle("GET /api/admin/users", authMW(middleware.RequireAdmin(http.HandlerFunc(adminHandler.ListUsers))))
	mux.Handle("PUT /api/admin/users/{id}/coins", authMW(middleware.RequireAdmin(http.HandlerFunc(adminHandler.AdjustCoins))))
	mux.Handle("GET /api/admin/characters", authMW(middleware.RequireAdmin(http.HandlerFunc(adminHandler.ListAllCharacters))))
	mux.Handle("DELETE /api/admin/characters/{id}", authMW(middleware.RequireAdmin(http.HandlerFunc(adminHandler.DeleteCharacter))))
	mux.Handle("GET /api/admin/reports", authMW(middleware.RequireAdmin(http.HandlerFunc(adminHandler.ListReports))))
	mux.Handle("PUT /api/admin/reports/{id}", authMW(middleware.RequireAdmin(http.HandlerFunc(adminHandler.UpdateReportStatus))))
	mux.Handle("GET /api/admin/stats", authMW(middleware.RequireAdmin(http.HandlerFunc(adminHandler.Stats))))

	addr := ":" + cfg.AppPort
	log.Printf("เซิร์ฟเวอร์เริ่มทำงานที่ http://localhost%s\n", addr)
	log.Fatal(http.ListenAndServe(addr, middleware.EnableCORS(mux)))
}
