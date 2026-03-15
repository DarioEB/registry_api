package main

import (
	"log"

	"github.com/DarioEB/logdeb"
)

func main() {
	logger, err := logdeb.New(logdeb.DefaultConfig())
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer logger.Close()

	logger.Info("Starting registry-dashboard-api")
	// Story 1.2: server setup, DB connection, migrations, Gin routes
}
