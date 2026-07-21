package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/entreya/nativestudio/context"
	"github.com/entreya/nativestudio/handlers"
)

// Config represents the application configuration
type Config struct {
	Port         int    `json:"port"`
	OllamaURL    string `json:"ollama_url"`
	DefaultModel string `json:"default_model"`
	Context      struct {
		YellowThreshold     float64 `json:"yellow_threshold"`
		RedThreshold        float64 `json:"red_threshold"`
		KeepRecentMessages  int     `json:"keep_recent_messages"`
		SummarizeUsingModel string  `json:"summarize_using_model"`
	} `json:"context"`
	Filesystem struct {
		RootDir           string   `json:"root_dir"`
		AllowedExtensions []string `json:"allowed_extensions"`
	} `json:"filesystem"`
}

func main() {
	// Load config
	configData, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Failed to read config.json: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(configData, &cfg); err != nil {
		log.Fatalf("Failed to parse config.json: %v", err)
	}

	// Ensure workspace directory exists
	if err := os.MkdirAll(cfg.Filesystem.RootDir, 0755); err != nil {
		log.Fatalf("Failed to create root directory %s: %v", cfg.Filesystem.RootDir, err)
	}

	// Create router
	mux := http.NewServeMux()

	// Register API handlers
	fileHandler := handlers.NewFileHandler(cfg.Filesystem.RootDir, cfg.Filesystem.AllowedExtensions)
	fileHandler.RegisterRoutes(mux)

	modelsHandler := handlers.NewModelsHandler(cfg.OllamaURL)
	modelsHandler.RegisterRoutes(mux)

	ctxCfg := context.Config{
		YellowThreshold:     cfg.Context.YellowThreshold,
		RedThreshold:        cfg.Context.RedThreshold,
		KeepRecentMessages:  cfg.Context.KeepRecentMessages,
		SummarizeUsingModel: cfg.Context.SummarizeUsingModel,
		OllamaURL:           cfg.OllamaURL,
	}

	chatHandler := handlers.NewChatHandler(cfg.OllamaURL, ctxCfg)
	chatHandler.RegisterRoutes(mux)

	// Serve static frontend files
	fs := http.FileServer(http.Dir("./frontend"))
	mux.Handle("/", fs)

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("NativeStudio backend starting up...")
	log.Printf("Listening on http://localhost%s", addr)
	log.Printf("Proxying Ollama at %s", cfg.OllamaURL)
	log.Printf("Workspace root: %s", cfg.Filesystem.RootDir)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
