# 🧠 nativestudio

> Continue.dev gives you AI in VS Code. This gives you control over what the AI actually remembers.

**nativestudio** is a local-first AI code editor — Monaco editor + Ollama backend with a persistent knowledge base of your codebase, an agentic tool-calling loop, checkpoint-based context summarization, and diff/patch-based code review. **No cloud. No subscriptions. No data leaves your machine.**

---

## Why This Exists

Most local AI editor tools (Continue.dev, VSCode Ollama) are great for quick suggestions but treat every conversation independently. They don't manage the growing context of a long coding session — when you're refactoring a complex module, fixing interconnected bugs, or building a feature over hours, the AI loses track of what it already decided.

**nativestudio** solves this with:

- A **tree-sitter-backed knowledge base** — every project is indexed into SQLite (symbols, chunks, embeddings, file/module/project summaries) and kept current by a file watcher, so the AI can retrieve relevant code instead of relying only on what's in the current chat.
- A **checkpoint system** for the conversation itself — tracks full history with token counts, summarizes old context into checkpoints before hitting the model's limit, and keeps recent messages verbatim.
- An **agent loop with tools** — the model can read files, search text, inspect editor state, and search the internet for current information, instead of only replying from a single prompt.

---

## Features

- 🖥️ **Split-view interface** — Monaco editor (left) + AI chat (right), built with React
- 🧠 **Context-aware sessions** — persisted history + retrieved knowledge sent to Ollama on every request
- 📚 **Project knowledge base** — tree-sitter indexing of symbols/chunks, semantic search via embeddings, project/module/file summaries, and a facts/decisions memory that survives across sessions
- 📍 **Checkpoint summarization** — AI compresses old context at 60–80% token usage
- 📊 **Token bar** — live visual of context usage with Green / Yellow / Red zones
- 🔀 **Diff & patch review** — AI-proposed edits are shown as diffs and staged as patches; accept, reject, or roll back
- 🛠️ **Agent tools** — file read/list/search, editor-state awareness, and internet search, all invoked by the model as native tool calls
- 📁 **File system access** — open and save files within a chosen project directory
- 🔌 **Model selector** — dropdown to switch between Ollama models mid-session, with optional "thinking" mode for models that support it
- 📡 **Streaming responses** — real-time output via SSE, no waiting for full reply
- 🔒 **100% offline, loopback-only by default** — no API keys, no telemetry; the server binds to `127.0.0.1` unless explicitly reconfigured

---

## Architecture

```
Browser (React + Monaco)
    ↕ REST + SSE
Go HTTP Server  ←→  File System
    │  ↕ SQLite (sessions, messages, knowledge, patches)
    │  ↕ tree-sitter indexer + fsnotify watcher
    ↕
Ollama (localhost:11434)
```

| Layer | Technology | Role |
|---|---|---|
| Frontend | React (Vite) + Monaco Editor + Ant Design | Editor, chat UI, diff/patch review |
| Backend | Go (net/http) | File I/O, sessions, agent loop, Ollama proxy |
| Persistence | SQLite (modernc.org/sqlite) | Sessions, messages, knowledge base, patches |
| Indexing | tree-sitter + fsnotify | Symbol/chunk extraction, incremental re-indexing |
| AI Runtime | Ollama | Local model inference, embeddings |

**The Go server owns all state** — sessions, messages, checkpoints, and the project knowledge base are persisted in SQLite (`data/nativestudio.db`), not just held in memory.

---

## Context Management

### Token Zones

```
0────────────────60%──────────80%──────100%
│   Green (safe)  │  Yellow   │  Red   │
│   Send as-is    │ Summarize │ Block  │
```

### Checkpoint Flow

1. Messages accumulate in the session's SQLite-backed store
2. At **60% token usage** → background summarization triggers
3. Oldest messages are sent to Ollama with a prompt to preserve key decisions, file names, function names, edited code, and error patterns
4. Summary stored as a **checkpoint** message (role: `system`); everything from the checkpoint threshold onward is left untouched, so messages appended while summarization is running are never lost
5. Recent messages are kept verbatim
6. Multiple checkpoints can stack — oldest compressed first

### Project Knowledge

Alongside conversation context, each project gets its own persistent knowledge base: indexed symbols and code chunks, optional embeddings for semantic search, generated file/module/project summaries, and a running list of facts, accepted/rejected decisions, and session learnings. This is what the agent's context resolver draws on before answering — see `/api/projects/{id}/knowledge` for the full picture.

---

## Prerequisites

- [Go](https://go.dev/dl/) 1.25+
- [Node.js](https://nodejs.org/) 18+ (to build the frontend)
- [Ollama](https://ollama.com) installed and running
- At least one model pulled in Ollama

```bash
# Pull a coding model (recommended)
ollama pull qwen2.5-coder:1.5b
# or
ollama pull deepseek-coder
# or
ollama pull qwen2.5-coder

# Pull an embedding model for semantic knowledge search
ollama pull nomic-embed-text
```

---

## Installation

```bash
# Clone the repo
git clone https://github.com/entreya/nativestudio.git
cd nativestudio

# Build the frontend (the Go server serves the built assets from frontend/dist)
cd frontend && npm install && npm run build && cd ..

# Install Go dependencies
go mod tidy

# Run the server
go run main.go
```

Server starts at `http://127.0.0.1:8080` by default — open in your browser.

During frontend development, run `npm run dev` inside `frontend/` for hot reload against the Go backend.

---

## Configuration

Edit `config.json` in the project root:

```json
{
  "host": "127.0.0.1",
  "port": 8080,
  "ollama_url": "http://localhost:11434",
  "db_path": "./data/nativestudio.db",
  "default_model": "qwen2.5-coder:1.5b",
  "chat_model": "qwen2.5-coder:1.5b",
  "summary_model": "qwen2.5-coder:1.5b",
  "embedding_model": "nomic-embed-text",
  "index_batch_size": 10,
  "maximum_file_size_bytes": 1048576,
  "ollama_concurrency": 2,
  "context": {
    "yellow_threshold": 0.60,
    "red_threshold": 0.80,
    "keep_recent_messages": 10,
    "summarize_using_model": "qwen2.5-coder:1.5b"
  },
  "filesystem": {
    "root_dir": "./workspace",
    "allowed_extensions": [".php", ".go", ".js", ".ts", ".py", ".md", ".json", ".yaml"]
  }
}
```

| Key | Default | Description |
|---|---|---|
| `host` | `127.0.0.1` | Listen address. Only change this if you understand that it exposes filesystem and tool-calling access to your network. |
| `db_path` | `./data/nativestudio.db` | SQLite database for sessions, messages, and the knowledge base |
| `summary_model` / `embedding_model` | same as chat model / `nomic-embed-text` | Models used for checkpoint summaries and semantic search |
| `index_batch_size` | 10 | Files enriched (summarized/embedded) per batch after indexing |
| `maximum_file_size_bytes` | 1048576 | Files larger than this are skipped by the indexer |
| `ollama_concurrency` | 2 | Max concurrent requests to Ollama for indexing work |
| `yellow_threshold` | 0.60 | Token % where summarization triggers |
| `red_threshold` | 0.80 | Token % where requests are blocked |
| `keep_recent_messages` | 10 | Messages kept verbatim before checkpoint |

**Security note:** the server is designed to run on your own machine only. It grants the model tool-calling access to read/write files under the active project, so avoid setting `host` to anything other than a loopback address unless you understand the risk.

---

## Usage

### Basic Flow

1. Open a folder to create or select a project
2. Open a file from the file tree (left panel) and write or paste code in Monaco
3. Type a prompt in the AI chat (right panel) — e.g., *"Refactor this function to handle errors explicitly"*
4. The agent may read files, search the codebase, or search the internet before responding — you can see each tool call in the timeline
5. Response streams in; proposed file edits appear as a **diff** — review and **Accept** or **Reject**
6. Accepted changes are applied to disk and staged as patches (see Project Knowledge → Patches) for later rollback if needed

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `Ctrl + Enter` | Send prompt |
| `Ctrl + S` | Save current file |

### Sending Context

- **Send full file** — checkbox in chat panel, included automatically for the active file
- **Editor state** — cursor position, selection, and the symbol under the cursor are sent with every request
- **Knowledge retrieval** — relevant symbols, chunks, and summaries from the project's indexed knowledge base are added automatically based on the prompt

---

## API Reference

The Go server exposes these endpoints (useful for scripting or custom clients):

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/projects` | List projects |
| `POST` | `/api/projects` | Create a project |
| `POST` | `/api/workspace` | Switch the active workspace root (loopback only) |
| `GET` | `/api/files` | List directory tree |
| `GET` | `/api/file?path=` | Read file content |
| `POST` | `/api/file` | Save file content |
| `GET` | `/api/models` | List available Ollama models |
| `POST` | `/api/chat` | Send prompt, stream response via SSE |
| `GET` | `/api/context` | Get current session context + token count |
| `POST` | `/api/context/reset` | Clear session context |
| `GET` | `/api/projects/{id}/sessions` | List sessions for a project |
| `GET` | `/api/sessions/{id}/messages` | Get a session's message history |
| `GET` | `/api/projects/{id}/knowledge` | Knowledge base overview (symbols, facts, decisions, summaries) |
| `POST` | `/api/projects/{id}/index` | Trigger a re-index |
| `GET` | `/api/projects/{id}/index/events` | SSE stream of indexing progress |
| `POST` | `/api/changes/{id}/approve` \| `/reject` | Approve or reject a proposed file change |
| `GET` | `/api/sessions/{sessionId}/patches` | List staged patches |
| `POST` | `/api/patches/{patchId}/rollback` | Roll back an applied patch |

---

## Project Structure

```
nativestudio/
├── main.go
├── config.json
├── agent/           # Tool-calling agent loop, tool registry, filesystem/search/internet tools
├── context/         # Session store, checkpoint summarizer, token estimation, context budgeting
├── db/              # SQLite access layer: sessions, messages, knowledge base, patches, migrations
├── editor/          # Editor-state persistence (cursor, selection, open/recent files)
├── handlers/        # HTTP handlers: files, chat, models, projects, sessions, knowledge, changes, patches
├── indexer/         # Tree-sitter parsing, file scanning/watching, incremental indexing pipeline
├── knowledge/       # Shared knowledge-base types and interfaces
├── ollama/          # Ollama client for embeddings and structured summarization
├── resolver/        # Resolves which file/symbol a prompt refers to, retrieves knowledge candidates
├── workspace/       # Path-traversal guard shared by every filesystem-touching component
├── frontend/        # React + Vite app (Monaco editor, chat panel, knowledge/conversations pages)
└── go.mod
```

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
