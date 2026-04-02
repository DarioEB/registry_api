package main

import (
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/DarioEB/logdeb"
	"github.com/gin-gonic/gin"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"registry_dashboard_api/config"
)

func main() {
	// 1. Logger
	logger, err := logdeb.New(logdeb.DefaultConfig())
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer logger.Close()
	logger.Info("Starting registry-dashboard-api")

	// 2. Config
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger.Info("Config loaded")

	// 3. Database
	db := connectDB(cfg, logger)
	logger.Info("Database connected")
	_ = db // will be used by handlers in subsequent stories

	// 4. Migrations
	runMigrations(cfg, logger)
	logger.Info("Migrations applied")

	// 5. Router
	r := gin.Default()
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// 6. Start server
	addr := fmt.Sprintf(":%s", cfg.Port)
	logger.Info("Server listening", "addr", addr)
	if err := r.Run(addr); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func connectDB(cfg *config.Config, logger *logdeb.Logdeb) *gorm.DB {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort,
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	return db
}

func runMigrations(cfg *config.Config, logger *logdeb.Logdeb) {
	migrateURL := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		url.QueryEscape(cfg.DBUser), url.QueryEscape(cfg.DBPassword),
		cfg.DBHost, cfg.DBPort, cfg.DBName,
	)
	m, err := migrate.New("file://migrations", migrateURL)
	if err != nil {
		logger.Error("failed to initialize migrations", "error", err)
		os.Exit(1)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
}
