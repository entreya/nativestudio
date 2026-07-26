# Contributing to nativestudio

Thanks for considering it. This is a small, opinionated project — for anything beyond a small fix, open an issue first so we don't both spend time on an approach that doesn't fit.

## Setting up

You'll need Go 1.25+, Node 18+, and [Ollama](https://ollama.com) running locally with at least one chat model and `nomic-embed-text` pulled — see the [README](README.md#prerequisites) and [MODELS.md](MODELS.md) for what to pull.

```bash
git clone https://github.com/entreya/nativestudio.git
cd nativestudio

cd frontend && npm install && cd ..
go mod tidy

go run main.go
```

For frontend work, run `npm run dev` inside `frontend/` alongside the Go server for hot reload.

## Before opening a PR

```bash
# Backend
go build ./...
go vet ./...
go test ./...

# Frontend
cd frontend
npm run lint
npm test
npm run build
```

All four (build, vet, test, lint) should be clean — CI runs the same checks and will block on any of them failing.

## What we look for in a change

- **New tools that mutate files or run commands must be staged for review**, not applied directly — see how `create_file`/`apply_patch`/`rename_symbol` in `agent/` stage a patch via `stagePatch` rather than writing to disk immediately. This is a hard rule, not a style preference: it's what makes the "review before it touches your code" model of this app actually true.
- **Tests for new agent/tool logic should be deterministic**, not dependent on a real Ollama server — see `agent/loop_test.go`'s `scriptedTransport` pattern for mocking model responses. A handful of tests are explicitly gated behind an env var for opt-in live verification against a real model (e.g. `agent/factcheck_live_test.go`); that pattern is fine for genuinely non-deterministic behavior, but should be the exception, not the default.
- **Bug fixes for anything agent-behavior-related should include the failure mode in the test**, not just the fix — a test named after what a user would hit is worth more than one named after the internal function. Several existing tests (e.g. `TestRenameSymbolFinishesAnAlreadyHalfDoneRename`) follow this pattern deliberately: the comment above each explains the real, observed failure, not just what the assertions check.
- **Small local models are the actual constraint this project designs around.** If a fix works by asking the model to "be more careful" in the system prompt, look for a structural fix instead (a deterministic tool, a clearer error message, a narrower prompt) before falling back to prompt tuning — prompting alone has a poor track record here.

## Commit style

Commit messages should explain *why*, not restate the diff — what broke, what the actual root cause was, and how it was verified. `git log` in this repo is a reasonably good example of the level of detail we're looking for.

## Reporting a bug

Include: what you asked the agent to do, what model you were using, and — if it's a tool-calling or reasoning issue — the reasoning/timeline trace if you have it (the chat panel's expandable timeline entries). "It didn't work" without that context is very hard to act on for anything model-behavior-related.
