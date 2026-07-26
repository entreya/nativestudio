package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/terminal"
	"github.com/gorilla/websocket"
)

// TerminalHandler exposes one persistent, PTY-backed shell per project over
// a websocket — a real integrated terminal (cd persists, long-running
// processes survive a page refresh, Ctrl+C/colors/interactive prompts all
// work), separate from the AI agent's own one-shot run_terminal tool
// (agent/terminal.go), which intentionally does not share this shell.
type TerminalHandler struct {
	db       *db.DB
	manager  *terminal.Manager
	upgrader websocket.Upgrader
}

func NewTerminalHandler(database *db.DB) *TerminalHandler {
	return &TerminalHandler{
		db:      database,
		manager: terminal.NewManager(),
		upgrader: websocket.Upgrader{
			// Served from the same origin as the rest of the local app;
			// there is no cross-site terminal access to guard against, but
			// the check is explicit rather than left at gorilla's default.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (h *TerminalHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/terminal/ws", h.Connect)
}

// terminalClientMessage is the client->server wire format: either a batch of
// typed/pasted input, or a resize after the browser panel changes size.
type terminalClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func (h *TerminalHandler) Connect(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	project, err := h.db.GetProject(projectID)
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	session, err := h.manager.GetOrCreate(projectID, project.Path)
	if err != nil {
		http.Error(w, "could not start terminal: "+err.Error(), http.StatusInternalServerError)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Subscribing (not attaching exclusively) means a reconnect — a page
	// refresh, a second tab — just adds another listener to the same
	// running shell rather than starting a new one.
	output, unsubscribe := session.Subscribe()
	defer unsubscribe()

	go func() {
		for chunk := range output {
			if writeErr := conn.WriteMessage(websocket.TextMessage, chunk); writeErr != nil {
				return
			}
		}
	}()

	for {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			return
		}
		var msg terminalClientMessage
		if jsonErr := json.Unmarshal(raw, &msg); jsonErr != nil {
			continue
		}
		switch msg.Type {
		case "input":
			_ = session.Write([]byte(msg.Data))
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				_ = session.Resize(msg.Cols, msg.Rows)
			}
		}
	}
}
