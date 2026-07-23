package main

import (
	stdcontext "context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/entreya/nativestudio/agent"
	"github.com/entreya/nativestudio/context"
	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"
	"github.com/entreya/nativestudio/handlers"
	"github.com/entreya/nativestudio/indexer"
	"github.com/entreya/nativestudio/knowledge"
	ollamaclient "github.com/entreya/nativestudio/ollama"
	"github.com/entreya/nativestudio/resolver"
)

// Config represents the application configuration
type Config struct {
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	OllamaURL            string `json:"ollama_url"`
	DBPath               string `json:"db_path"`
	DefaultModel         string `json:"default_model"`
	ChatModel            string `json:"chat_model"`
	SummaryModel         string `json:"summary_model"`
	EmbeddingModel       string `json:"embedding_model"`
	IndexBatchSize       int    `json:"index_batch_size"`
	MaximumFileSizeBytes int64  `json:"maximum_file_size_bytes"`
	OllamaConcurrency    int    `json:"ollama_concurrency"`
	MaxAgentToolSteps    int    `json:"max_agent_tool_steps"`
	Context              struct {
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
	if cfg.ChatModel != "" {
		cfg.DefaultModel = cfg.ChatModel
	}

	// Ensure workspace directory exists
	if err := os.MkdirAll(cfg.Filesystem.RootDir, 0755); err != nil {
		log.Fatalf("Failed to create root directory %s: %v", cfg.Filesystem.RootDir, err)
	}

	// Ensure data directory exists
	if err := os.MkdirAll("./data", 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Initialize DB
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Replace in-memory store
	context.Store = context.NewDBStore(database)

	// Create router
	mux := http.NewServeMux()

	// Register API handlers
	fileHandler := handlers.NewFileHandler(cfg.Filesystem.RootDir, cfg.Filesystem.AllowedExtensions)
	fileHandler.RegisterRoutes(mux)

	modelsHandler := handlers.NewModelsHandler(cfg.OllamaURL, cfg.DefaultModel)
	modelsHandler.RegisterRoutes(mux)

	systemHandler := handlers.NewSystemHandler(cfg.Filesystem.AllowedExtensions)
	systemHandler.RegisterRoutes(mux)

	ctxCfg := context.Config{
		YellowThreshold:     cfg.Context.YellowThreshold,
		RedThreshold:        cfg.Context.RedThreshold,
		KeepRecentMessages:  cfg.Context.KeepRecentMessages,
		SummarizeUsingModel: cfg.Context.SummarizeUsingModel,
		OllamaURL:           cfg.OllamaURL,
	}

	// Phase 8: Editor context service + reference resolver
	editorSvc := editor.NewEditorContextService(database)
	res := resolver.NewReferenceResolver(cfg.Filesystem.RootDir, database)

	registry := agent.NewRegistry(cfg.Filesystem.RootDir)
	if cfg.SummaryModel == "" {
		cfg.SummaryModel = cfg.DefaultModel
	}
	if cfg.EmbeddingModel == "" {
		cfg.EmbeddingModel = "nomic-embed-text"
	}
	modelClient := ollamaclient.NewClient(cfg.OllamaURL, cfg.SummaryModel, cfg.EmbeddingModel, cfg.OllamaConcurrency)
	scanConfig := indexer.DefaultScanConfig()
	if cfg.MaximumFileSizeBytes > 0 {
		scanConfig.MaximumFileSizeBytes = cfg.MaximumFileSizeBytes
	}
	indexBroker := indexer.NewBroker()
	indexCoordinator := &indexer.Coordinator{DB: database, Scanner: indexer.Scanner{Config: scanConfig}, Parser: indexer.TreeSitterParser{Fallback: indexer.StructuralParser{}}, Chunker: indexer.SymbolChunker{}, Models: modelClient, SummaryModel: cfg.SummaryModel, EmbeddingModel: cfg.EmbeddingModel, BatchSize: cfg.IndexBatchSize, Broker: indexBroker}
	fileHandler.SetWorkspaceChangeHandler(func(projectID, path string) {
		registry.SetWorkspaceRoot(path)
		res.SetWorkspaceRoot(path)
		res.SetKnowledgeWorkspace(projectID, modelClient, cfg.EmbeddingModel)
		if projectID != "" {
			defaults := knowledge.IndexSettings{ExcludePaths: scanConfig.ExcludedDirectories, SensitivePatterns: scanConfig.SensitivePatterns, MaximumFileSizeBytes: scanConfig.MaximumFileSizeBytes}
			if settings, settingsErr := database.GetIndexSettings(stdcontext.Background(), projectID, defaults); settingsErr == nil {
				indexCoordinator.Configure(settings)
			}
			indexCoordinator.Start(projectID, path)
		}
	})
	agentRunner := agent.NewAgent(cfg.OllamaURL, registry, database, editorSvc, res, cfg.MaxAgentToolSteps)

	chatHandler := handlers.NewChatHandler(cfg.OllamaURL, ctxCfg, agentRunner, database)
	chatHandler.RegisterRoutes(mux)

	projectsHandler := handlers.NewProjectsHandler(database)
	projectsHandler.RegisterRoutes(mux)

	sessionsHandler := handlers.NewSessionsHandler(database)
	sessionsHandler.RegisterRoutes(mux)
	knowledgeHandler := handlers.NewKnowledgeHandler(database, indexCoordinator, indexBroker)
	knowledgeHandler.RegisterRoutes(mux)
	changesHandler := handlers.NewChangesHandler(database, indexCoordinator)
	changesHandler.RegisterRoutes(mux)
	patchHandler := handlers.NewPatchHandler(database, indexCoordinator)
	patchHandler.RegisterRoutes(mux)
	commandHandler := handlers.NewCommandHandler(database, indexCoordinator)
	commandHandler.RegisterRoutes(mux)

	// Serve static frontend files (from the React build). The frontend is a
	// client-routed SPA (react-router), so any path that isn't a real file in
	// dist/ (e.g. /projects/abc123) must still resolve to index.html rather
	// than 404 — otherwise a deep link or hard refresh on a client route breaks.
	const staticDir = "./frontend/dist"
	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("/", func() http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			full := filepath.Join(staticDir, filepath.Clean(r.URL.Path))
			if info, err := os.Stat(full); err != nil || info.IsDir() {
				http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
				return
			}
			fileServer.ServeHTTP(w, r)
		}
	}())

	// Start server. Default to loopback-only: this app grants an LLM tool-calling
	// access to the local filesystem, so it must not be reachable from the network
	// unless the operator explicitly opts in via config.json's "host".
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", host, cfg.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No ReadTimeout/WriteTimeout: chat responses stream over SSE for as long
		// as the model takes to finish, which can exceed any fixed deadline.
	}

	log.Printf("NativeStudio backend starting up...")
	log.Printf("Listening on http://%s", addr)
	log.Printf("Proxying Ollama at %s", cfg.OllamaURL)
	log.Printf("Workspace root: %s", cfg.Filesystem.RootDir)
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		log.Printf("WARNING: listening on %s exposes filesystem and tool-calling access to your network. Only do this if you understand the risk.", host)
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Fatalf("Server failed: %v", err)
	case sig := <-stop:
		log.Printf("Received %s, shutting down...", sig)
	}

	shutdownCtx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Graceful shutdown failed: %v", err)
	}
	indexCoordinator.Cancel()
	if err := database.Close(); err != nil {
		log.Printf("Failed to close database: %v", err)
	}
	log.Printf("Shutdown complete.")
}
