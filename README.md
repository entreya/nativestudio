# 🧠 nativestudio

> Continue.dev gives you AI in VS Code. This gives you control over what the AI actually remembers.

**nativestudio** is a local-first AI code editor — Monaco editor + Ollama backend with checkpoint-based context summarization, token-aware session management, and diff-based code review. **No cloud. No subscriptions. No data leaves your machine.**

---

## Why This Exists

Most local AI editor tools (Continue.dev, VSCode Ollama) are great for quick suggestions but treat every conversation independently. They don't manage the growing context of a long coding session — when you're refactoring a complex module, fixing interconnected bugs, or building a feature over hours, the AI loses track of what it already decided.

**nativestudio** solves this with a checkpoint system:

- Tracks your full conversation history with token count
- Automatically summarizes old context into checkpoints before hitting the model's limit
- Keeps recent messages verbatim, compresses older ones intelligently
- Every edit, decision, and pattern the AI noticed is preserved — not dropped

---

## Features

- 🖥️ **Split-view interface** — Monaco editor (left) + AI chat (right)
- 🧠 **Context-aware sessions** — full message history sent to Ollama on every request
- 📍 **Checkpoint summarization** — AI compresses old context at 60–80% token usage
- 📊 **Token bar** — live visual of context usage with Green / Yellow / Red zones
- 🔀 **Diff-based edits** — AI suggestions shown as diffs; accept or reject line by line
- 📁 **File system access** — open and save `.php`, `.go`, and other files directly
- 🔌 **Model selector** — dropdown to switch between Ollama models mid-session
- 📡 **Streaming responses** — real-time output via SSE, no waiting for full reply
- 🔒 **100% offline** — no API keys, no telemetry, no external requests

---

## Architecture

```
Browser (Frontend)
    ↕ REST + SSE
Go HTTP Server  ←→  File System
    ↕
Ollama (localhost:11434)
```

| Layer | Technology | Role |
|---|---|---|
| Frontend | HTML + Monaco Editor + Vanilla JS | Editor, chat UI, diff viewer |
| Backend | Go (net/http) | File I/O, context store, Ollama proxy |
| AI Runtime | Ollama | Local model inference |

**The Go server owns the context** — not the browser. Sessions are stored in-memory with full message history, token estimates, and checkpoint metadata.

---

## Context Management

This is what sets nativestudio apart.

### Token Zones

```
0────────────────60%──────────80%──────100%
│   Green (safe)  │  Yellow   │  Red   │
│   Send as-is    │ Summarize │ Block  │
```

### Checkpoint Flow

1. Messages accumulate in session store
2. At **60% token usage** → background summarization triggers
3. Oldest N messages sent to Ollama with prompt: *"Summarize preserving key decisions, file names, function names, edited code, error patterns"*
4. Summary stored as a **checkpoint** message (role: `system`)
5. Recent N messages kept verbatim
6. Multiple checkpoints can stack — oldest compressed first

### Checkpoint Structure

```json
{
  "type": "checkpoint",
  "created_at": "2025-07-21T10:30:00Z",
  "tokens_before": 12000,
  "tokens_after": 800,
  "summary": "Working on auth.go. Refactored Login() to use JWT. Fixed nil pointer on line 42. User prefers explicit error returns over panic."
}
```

---

## Prerequisites

- [Go](https://go.dev/dl/) 1.21+
- [Ollama](https://ollama.com) installed and running
- At least one model pulled in Ollama

```bash
# Pull a coding model (recommended)
ollama pull codellama
# or
ollama pull deepseek-coder
# or
ollama pull qwen2.5-coder
```

---

## Installation

```bash
# Clone the repo
git clone https://github.com/entreya/nativestudio.git
cd nativestudio

# Install Go dependencies
go mod tidy

# Run the server
go run main.go
```

Server starts at `http://localhost:8080` — open in your browser.

---

## Configuration

Edit `config.json` in the project root:

```json
{
  "port": 8080,
  "ollama_url": "http://localhost:11434",
  "default_model": "codellama",
  "context": {
    "yellow_threshold": 0.60,
    "red_threshold": 0.80,
    "keep_recent_messages": 10,
    "summarize_using_model": "llama3"
  },
  "filesystem": {
    "root_dir": "./workspace",
    "allowed_extensions": [".php", ".go", ".js", ".ts", ".py", ".md", ".json", ".yaml"]
  }
}
```

| Key | Default | Description |
|---|---|---|
| `yellow_threshold` | 0.60 | Token % where summarization triggers |
| `red_threshold` | 0.80 | Token % where requests are blocked |
| `keep_recent_messages` | 10 | Messages kept verbatim before checkpoint |
| `summarize_using_model` | same model | Can use a lighter model for summarization |

---

## Usage

### Basic Flow

1. Open a file from the file tree (left panel)
2. Write or paste code in Monaco editor
3. Type a prompt in the AI chat (right panel) — e.g., *"Refactor this function to handle errors explicitly"*
4. AI response streams in; if it includes code changes, a **diff modal** appears
5. Review the diff line by line → **Accept** or **Reject**
6. Accepted changes apply to the editor; the exchange is added to context

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `Ctrl + Enter` | Send prompt |
| `Ctrl + S` | Save current file |
| `Ctrl + Shift + C` | Clear context / start new session |
| `Ctrl + K` | Send selected code as context |

### Sending Context

- **Send full file** — checkbox in chat panel
- **Send selection** — select code in editor, then send prompt
- **Send with path** — file path always included so AI knows what it's editing

---

## API Reference

The Go server exposes these endpoints (useful for scripting or custom clients):

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/files` | List directory tree |
| `GET` | `/api/file?path=` | Read file content |
| `POST` | `/api/file` | Save file content |
| `GET` | `/api/models` | List available Ollama models |
| `POST` | `/api/chat` | Send prompt, stream response via SSE |
| `GET` | `/api/context` | Get current session context + token count |
| `POST` | `/api/context/reset` | Clear session context |

---

## Project Structure

```
nativestudio/
├── main.go
├── config.json
├── handlers/
│   ├── files.go          # File system CRUD
│   ├── chat.go           # Ollama proxy + SSE streaming
│   └── models.go         # Model listing
├── context/
│   ├── store.go          # In-memory session store
│   ├── summarizer.go     # Checkpoint logic
│   └── tokens.go         # Token estimation
├── frontend/
│   ├── index.html
│   ├── editor.js         # Monaco setup
│   ├── chat.js           # SSE + chat rendering
│   ├── diff.js           # Diff modal + accept/reject
│   └── style.css
└── go.mod
```

---

## Roadmap

- [ ] Phase 1 — Go server + file system endpoints
- [ ] Phase 2 — Context engine + checkpoint summarization
- [ ] Phase 3 — Monaco editor + split layout
- [ ] Phase 4 — AI chat panel + streaming
- [ ] Phase 5 — Diff modal + accept/reject
- [ ] Multi-file context (send multiple open files)
- [ ] Persistent sessions (save/restore across restarts)
- [ ] Custom system prompt per project
- [ ] Support for OpenAI-compatible APIs (LM Studio, Jan)

---

## Contributing

PRs welcome. For major changes, open an issue first to discuss what you'd like to change.

```bash
# Fork → Clone → Branch
git checkout -b feature/your-feature-name

# Make changes, then
git commit -m "feat: your feature description"
git push origin feature/your-feature-name
# Open PR
```

---

## License

MIT — do whatever you want, just keep the attribution.

---

<p align="center">Built for developers who want AI assistance without giving away their code.</p>
