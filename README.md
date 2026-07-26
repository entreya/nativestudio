# 🧠 nativestudio

> Local models are small on purpose — the reliability has to come from the harness around them, not from pretending they're bigger than they are.

**nativestudio** is a local-first AI code editor — Monaco editor + Ollama backend with a persistent knowledge base of your codebase, an agentic tool-calling loop, verified/fact-checked memory, a real terminal, and diff/patch-based code review. **No cloud. No subscriptions. No data leaves your machine.**

---

## Why This Exists

A 4B-parameter local model on a laptop is never going to out-think a frontier model — it doesn't have the capacity, and no amount of prompting closes that gap. What it *can* do is stop making the same class of mistake twice, if the mistakes it's prone to are handled structurally instead of left to its judgment:

- Ask it to rename a class by hand-editing text, and it will burn most of its reasoning re-deriving an exact string match, then still forget to rename the file — breaking PSR-4/module resolution. Give it a `rename_symbol` tool that takes a name instead of a string, and that entire failure mode disappears.
- Ask it a question with a stale or unverified answer already in its context, and it will confidently repeat it. Gate what gets remembered behind cross-source agreement or explicit user confirmation, and it can't.
- Ask it something and disconnect before it answers (switch tabs, close a panel), and a naive implementation just discards the in-flight response. Detach the run from the request lifecycle, and it doesn't.
- Leave it with an ambiguous tool error ("not found") for a state it can't interpret, and it will spend several rounds re-guessing what happened. Give the tool enough context to say what's actually true, and it reports the answer in one line instead.

None of this makes the model smarter. It shrinks the set of situations where the model's own judgment is the thing standing between you and a correct result. That's the actual bet this project makes, and most of what's below follows from it.

---

## Features

- 🖥️ **Split-view interface** — Monaco editor (left) + AI chat (right) + a real PTY-backed terminal panel, all in one window
- 🧠 **Context-aware sessions** — persisted history + retrieved knowledge sent to Ollama on every request, with checkpoint summarization before the context window fills
- 📚 **Project knowledge base** — tree-sitter indexing of symbols/chunks, semantic search via embeddings, project/module/file summaries kept current by a file watcher
- ✅ **Verified memory, not just history** — facts only get recalled as settled once they've passed cross-source agreement (independent publisher domains, not raw result count) or explicit user confirmation; a separate table from ordinary session history so an unverified guess can never be replayed as fact
- 🌐 **Multi-engine fact-checked search** — queries Bing, Google News, Wikipedia, and DuckDuckGo in parallel, unwraps aggregator links to their real publisher, and marks derivative sources (e.g. DuckDuckGo's abstract, largely Wikipedia text) so they can't falsely corroborate each other
- 🪜 **Confidence escalation ladder** — high confidence → answer and cite it; medium → answer with the corroboration caveat stated; low → say so and offer to search again or ask you, instead of asserting a weak claim as settled
- 🛠️ **Deterministic refactor tools** — `rename_symbol` renames a class/function/constant's declaration, every reference, and its file together in one atomic, reviewable operation — no hand-written find/replace, no PSR-4 mismatches
- 💻 **Real integrated terminal** — a persistent PTY per project (cd survives, long processes outlive the panel closing, scrollback replays on reconnect) alongside `run_terminal` for the agent's own quick, reversible commands
- 📍 **Checkpoint summarization** — AI compresses old context at 60–80% token usage without losing recent messages
- 📊 **Token bar** — live visual of context usage with Green / Yellow / Red zones
- 🔀 **Diff & patch review** — every proposed edit (including multi-file operations like a rename) is shown as a diff and staged as a patch; accept, reject, or roll back
- 🗄️ **Database explorer** — browse the app's own SQLite state (sessions, knowledge, patches) with real pagination, not a silently-truncated first page
- 🎨 **Platform-matched themes** — macOS, Windows 11, Windows 12, and Liquid Glass, plus a Windows-11-style snap layout picker
- ⚙️ **Tunable agent behavior** — Settings exposes max thinking-token budget, response temperature ("creativity"), and periodic model unload, all read live with no restart
- 📡 **Streaming responses** — real-time output via SSE, with the full reasoning timeline (tool calls, search results, rephrasing) persisted and replayed on reload
- 🔒 **100% offline, loopback-only by default** — no API keys, no telemetry; the server binds to `127.0.0.1` unless explicitly reconfigured

---

## Architecture

```
Browser (React + Monaco + xterm.js)
    ↕ REST + SSE + WebSocket
Go HTTP Server  ←→  File System  ←→  PTY (terminal sessions)
    │  ↕ SQLite (sessions, messages, knowledge, patches, verified facts)
    │  ↕ tree-sitter indexer + fsnotify watcher
    ↕
Ollama (localhost:11434)
```

| Layer | Technology | Role |
|---|---|---|
| Frontend | React (Vite) + Monaco Editor + xterm.js + Ant Design | Editor, chat UI, terminal, diff/patch review |
| Backend | Go (net/http) | File I/O, sessions, agent loop, Ollama proxy, PTY management |
| Persistence | SQLite (modernc.org/sqlite) | Sessions, messages, knowledge base, patches, verified facts |
| Indexing | tree-sitter + fsnotify | Symbol/chunk extraction, incremental re-indexing |
| AI Runtime | Ollama | Local model inference, embeddings |

**The Go server owns all state** — sessions, messages, checkpoints, verified facts, and the project knowledge base are persisted in SQLite (`data/nativestudio.db`), not just held in memory.

---

## Agent Tools

The model never edits files or runs commands by generating raw text a person then has to trust — every mutating action goes through a specific tool, gets staged as a reviewable patch, and is only applied once approved:

| Tool | What it does |
|---|---|
| `find_files`, `list_directory` | Locate files by name/pattern before assuming a path |
| `search_text` | Grep-style search across the workspace |
| `search_internet` | Multi-engine, cross-source, confidence-scored web search |
| `read_file`, `read_file_range` | Read exact file content instead of guessing at it |
| `get_editor_context` | The active file, cursor, selection — what you're actually looking at |
| `create_file`, `delete_file`, `replace_in_file`, `apply_patch` | Staged, reviewable single-file mutations |
| `rename_symbol` | Deterministic rename: declaration + every reference + the file itself, together |
| `run_command`, `run_terminal` | Shell execution — instant for reversible commands, staged for approval otherwise |
| `ask_follow_up` | A real clarifying question when the request is genuinely ambiguous — not a fallback for "I didn't feel like acting" |

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

### Project Knowledge & Verified Memory

Each project gets its own persistent knowledge base: indexed symbols and code chunks, optional embeddings for semantic search, generated file/module/project summaries, and a running list of facts, accepted/rejected decisions, and session learnings — see `/api/projects/{id}/knowledge` for the full picture.

Two things are kept structurally separate from that ordinary history: **verified facts** (only written after cross-source search agreement or your explicit confirmation, so recalling one is safe) and **approved changes** (procedural memory drawn only from patches you actually accepted — never from the model's own unverified claim that something worked).

---

## Prerequisites

- [Go](https://go.dev/dl/) 1.25+
- [Node.js](https://nodejs.org/) 18+ (to build the frontend)
- [Ollama](https://ollama.com) installed and running
- At least one model pulled in Ollama

```bash
# A small, capable coding model — Qwen3 4B is what nativestudio ships a
# custom Modelfile for by default (see below); any Ollama chat model works
ollama pull qwen3:4b

# An embedding model for semantic knowledge search
ollama pull nomic-embed-text
```

**Choosing a model:** for a coding agent, reliable *tool-calling* matters more than raw benchmark score — a model that narrates a code change instead of calling a tool is worse than a smaller one that reliably calls the right tool every time. Dense models have consistently outperformed Mixture-of-Experts models of similar or larger size on multi-step tool-calling in independent 2026 evaluations; start with a small dense model in the 4B–8B range rather than a flashier MoE model that needs far more RAM than its "active parameter count" suggests.

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
  "default_model": "qwen3:4b",
  "chat_model": "qwen3:4b",
  "summary_model": "qwen3:4b",
  "embedding_model": "nomic-embed-text",
  "index_batch_size": 10,
  "maximum_file_size_bytes": 1048576,
  "ollama_concurrency": 2,
  "embedding_concurrency": 4,
  "max_agent_tool_steps": 15,
  "context": {
    "yellow_threshold": 0.6,
    "red_threshold": 0.8,
    "keep_recent_messages": 10,
    "summarize_using_model": "qwen3:4b"
  },
  "filesystem": {
    "root_dir": "./workspace",
    "allowed_extensions": ["...see config.json for the full list..."]
  }
}
```

| Key | Description |
|---|---|
| `host` | Listen address. Only change this if you understand that it exposes filesystem and tool-calling access to your network. |
| `db_path` | SQLite database for sessions, messages, knowledge base, and verified facts |
| `summary_model` / `embedding_model` | Models used for checkpoint summaries and semantic search |
| `max_agent_tool_steps` | Hard ceiling on tool-call steps per agent run, independent of the thinking-token budget |
| `index_batch_size` | Files enriched (summarized/embedded) per batch after indexing |
| `maximum_file_size_bytes` | Files larger than this are skipped by the indexer |
| `ollama_concurrency` / `embedding_concurrency` | Max concurrent requests to Ollama for indexing/enrichment work |
| `yellow_threshold` / `red_threshold` | Token % where summarization triggers / requests are blocked |
| `keep_recent_messages` | Messages kept verbatim before checkpoint |

Beyond `config.json`, the in-app **Settings** page lets you tune agent behavior live, no restart required: max thinking-token budget, response temperature ("Response Creativity"), and how often the model is force-unloaded from memory under sustained use.

**Security note:** the server is designed to run on your own machine only. It grants the model tool-calling access to read/write files under the active project and to run shell commands, so avoid setting `host` to anything other than a loopback address unless you understand the risk.

---

## Usage

### Basic Flow

1. Open a folder to create or select a project
2. Open a file from the file tree (left panel) and write or paste code in Monaco
3. Type a prompt in the AI chat (right panel) — e.g., *"Rename UserController to AccountController"*
4. The agent may read files, search the codebase, or search the internet before responding — every tool call is visible in the reasoning timeline
5. Response streams in; proposed file edits appear as a **diff** — review and **Accept** or **Reject**
6. Accepted changes are applied to disk and staged as patches (see Database Explorer → patches) for later rollback if needed

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `Ctrl + Enter` | Send prompt |
| `Ctrl + S` | Save current file |

### Sending Context

- **Send full file** — checkbox in chat panel, included automatically for the active file
- **Editor state** — cursor position, selection, and the symbol under the cursor are sent with every request
- **Knowledge retrieval** — relevant symbols, chunks, and summaries from the project's indexed knowledge base are added automatically based on the prompt
- **Verified facts & approved changes** — recalled automatically when the current prompt matches something already settled, so the model doesn't re-research or re-derive it from scratch

---

## API Reference

The Go server exposes these endpoints (useful for scripting or custom clients):

**Projects & Files**
| Method | Endpoint | Description |
|---|---|---|
| `GET`/`POST` | `/api/projects` | List / create projects |
| `GET`/`DELETE` | `/api/projects/{id}` | Get / delete a project |
| `GET` | `/api/files` | List directory tree |
| `GET`/`POST` | `/api/file` | Read / save file content |
| `POST` | `/api/files/create` \| `/rename` \| `/delete` | File operations from the explorer UI |
| `GET` | `/api/system/browse` | Native folder picker (Open Folder) |

**Chat & Sessions**
| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/chat` | Send prompt, stream response via SSE |
| `GET`/`POST` | `/api/projects/{id}/sessions` | List / create sessions |
| `GET`/`DELETE` | `/api/sessions/{id}` | Session messages / delete |
| `GET` | `/api/context` | Current session context + token count |
| `POST` | `/api/context/reset` | Clear session context |
| `GET` | `/api/models` | List available Ollama models |
| `POST` | `/api/models/unload` | Free a model from memory immediately |

**Knowledge & Memory**
| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/projects/{id}/knowledge` | Symbols, facts, decisions, summaries |
| `POST` | `/api/projects/{id}/index` | Trigger a re-index |
| `GET` | `/api/projects/{id}/index/status` \| `/events` | Index progress (poll or SSE) |
| `POST` | `/api/projects/{id}/index/stop` | Cancel an in-progress index |
| `POST` | `/api/projects/{id}/knowledge/enrichment/approve` \| `/decline` \| `/pause` \| `/resume` | Enrichment gate controls |
| `PATCH`/`DELETE` | `/api/projects/{id}/knowledge/facts/{factID}` | Edit / remove a stored fact |

**Patches & Commands**
| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/sessions/{sessionId}/patches` | List staged patches |
| `POST` | `/api/sessions/{sessionId}/patches/approve` \| `/reject` | Resolve a staged patch |
| `POST` | `/api/patches/{patchId}/rollback` | Roll back an applied patch |
| `GET` | `/api/sessions/{sessionId}/commands` | List staged commands |
| `POST` | `/api/sessions/{sessionId}/commands/approve` \| `/reject` | Resolve a staged command |

**Terminal, Database & Settings**
| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/projects/{id}/terminal/ws` | WebSocket for the real PTY terminal panel |
| `GET` | `/api/projects/{id}/db/tables` \| `/tables/{table}/data` | Browse the app's own SQLite state |
| `GET`/`PUT` | `/api/settings` | Read / update agent + editor settings |

---

## Project Structure

```
nativestudio/
├── main.go
├── config.json
├── agent/           # Tool-calling agent loop, tool registry, rename/rephrase/search/verified-memory logic
├── context/         # Session store, checkpoint summarizer, token estimation, context budgeting
├── db/              # SQLite access layer: sessions, messages, knowledge base, patches, verified facts, migrations
├── editor/          # Editor-state persistence (cursor, selection, open/recent files)
├── handlers/        # HTTP handlers: files, chat, models, projects, sessions, knowledge, patches, terminal, settings, database
├── indexer/         # Tree-sitter parsing, file scanning/watching, incremental indexing pipeline
├── knowledge/       # Shared knowledge-base types and interfaces
├── ollama/          # Ollama client for embeddings and structured summarization
├── resolver/        # Resolves which file/symbol a prompt refers to, retrieves knowledge candidates
├── terminal/        # PTY session management for the integrated terminal panel
├── workspace/       # Path-traversal guard shared by every filesystem-touching component
├── frontend/        # React + Vite app (Monaco editor, chat panel, terminal, knowledge/database/settings pages)
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

[MIT](LICENSE) — do whatever you want, just keep the attribution.

---

<p align="center">Built for developers who want AI assistance without giving away their code.</p>
