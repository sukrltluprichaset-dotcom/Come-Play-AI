package main

import (
	"log"

	"github.com/gofiber/fiber/v2"

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

	app := fiber.New(fiber.Config{
		BodyLimit: 20 << 20, // 20 MB (ให้พอสำหรับอัปโหลดรูป/วิดีโอสั้นๆ ใน /api/uploads)
	})

	app.Use(middleware.EnableCORS())

	app.Get("/health", func(c *fiber.Ctx) error {
		count, err := database.PackageCount(db)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"status":          "error",
				"database_status": "disconnected",
			})
		}
		return c.JSON(fiber.Map{
			"status":          "ok",
			"database_status": "connected",
			"package_count":   count,
		})
	})

	authMW := middleware.RequireAuth(cfg.JWTSecret)

	authHandler := handlers.NewAuthHandler(db, cfg.JWTSecret)
	app.Post("/api/auth/register", authHandler.Register)
	app.Post("/api/auth/login", authHandler.Login)
	app.Put("/api/auth/password", authMW, authHandler.ChangePassword)
	app.Put("/api/profile", authMW, authHandler.UpdateProfile)

	characterHandler := handlers.NewCharacterHandler(db)
	app.Post("/api/characters", authMW, characterHandler.Create)
	app.Get("/api/characters", authMW, characterHandler.ListMine)
	app.Get("/api/characters/public", characterHandler.ListPublic)
	app.Get("/api/characters/popular", characterHandler.Popular)
	app.Get("/api/characters/stats", authMW, characterHandler.Stats)
	app.Get("/api/characters/:id", authMW, characterHandler.Get)
	app.Put("/api/characters/:id", authMW, characterHandler.Update)
	app.Delete("/api/characters/:id", authMW, characterHandler.Delete)

	favoriteHandler := handlers.NewFavoriteHandler(db)
	app.Post("/api/characters/:id/favorite", authMW, favoriteHandler.Add)
	app.Delete("/api/characters/:id/favorite", authMW, favoriteHandler.Remove)
	app.Get("/api/favorites", authMW, favoriteHandler.List)

	reviewHandler := handlers.NewReviewHandler(db)
	app.Post("/api/characters/:id/reviews", authMW, reviewHandler.CreateOrUpdate)
	app.Get("/api/characters/:id/reviews", reviewHandler.List)

	reportHandler := handlers.NewReportHandler(db)
	app.Post("/api/characters/:id/reports", authMW, reportHandler.Create)

	evaluationHandler := handlers.NewEvaluationHandler(db)
	app.Post("/api/evaluations", authMW, evaluationHandler.Submit)

	uploadHandler := handlers.NewUploadHandler(cfg.SupabaseURL, cfg.SupabaseServiceKey, cfg.SupabaseBucket)
	app.Post("/api/uploads", authMW, uploadHandler.Upload)
	app.Static("/uploads", "./uploads")

	geminiClient := llm.NewGeminiClient(cfg.GeminiAPIKey)
	chatHandler := handlers.NewChatHandler(db, geminiClient)
	app.Post("/api/characters/:id/chats", authMW, chatHandler.SendMessage)
	app.Get("/api/characters/:id/chats", authMW, chatHandler.GetHistory)

	coinHandler := handlers.NewCoinHandler(db)
	app.Get("/api/coins", authMW, coinHandler.GetBalance)

	activityHandler := handlers.NewActivityHandler(db)
	app.Get("/api/activities", activityHandler.List)
	app.Post("/api/activities/:id/claim", authMW, activityHandler.Claim)

	diaryHandler := handlers.NewDiaryHandler(db, geminiClient)
	app.Post("/api/characters/:id/diary", authMW, diaryHandler.Generate)
	app.Get("/api/diaries", authMW, diaryHandler.List)

	adminHandler := handlers.NewAdminHandler(db)
	app.Get("/api/admin/users", authMW, middleware.RequireAdmin, adminHandler.ListUsers)
	app.Put("/api/admin/users/:id/suspend", authMW, middleware.RequireAdmin, adminHandler.SuspendUser)
	app.Delete("/api/admin/users/:id", authMW, middleware.RequireAdmin, adminHandler.DeleteUser)
	app.Get("/api/admin/characters", authMW, middleware.RequireAdmin, adminHandler.ListAllCharacters)
	app.Delete("/api/admin/characters/:id", authMW, middleware.RequireAdmin, adminHandler.DeleteCharacter)
	app.Get("/api/admin/reports", authMW, middleware.RequireAdmin, adminHandler.ListReports)
	app.Put("/api/admin/reports/:id", authMW, middleware.RequireAdmin, adminHandler.UpdateReportStatus)
	app.Get("/api/admin/stats", authMW, middleware.RequireAdmin, adminHandler.Stats)

	paymentHandler := handlers.NewPaymentHandler(db)
	app.Get("/api/packages", paymentHandler.ListPackages)
	app.Post("/api/payments", authMW, paymentHandler.CreatePayment)
	app.Get("/api/payments", authMW, paymentHandler.ListMyPayments)

	addr := ":" + cfg.AppPort
	log.Printf("เซิร์ฟเวอร์เริ่มทำงานที่ http://localhost%s\n", addr)
	log.Fatal(app.Listen(addr))
}
