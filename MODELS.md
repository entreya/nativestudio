# Choosing a Model

nativestudio talks to whatever Ollama model you point it at, but not every model is a good fit for an agent that has to reliably call tools (`create_file`, `apply_patch`, `rename_symbol`, `run_terminal`, ...) rather than just chat. This is a short, opinionated guide based on what's actually been observed running this app, not a general LLM leaderboard.

## The one thing that matters most: tool-calling reliability

A model that narrates a code change in prose instead of calling `apply_patch` is worse than a smaller model that reliably calls the right tool every time — narrated changes never actually reach your files. Raw coding benchmarks (HumanEval, etc.) don't measure this; a model can be excellent at writing code and still be unreliable at *deciding when and how to call a tool* about it.

An independent 2026 evaluation of local models on multi-step tool-calling found a clear pattern: **dense models decisively outperform Mixture-of-Experts (MoE) models of similar or larger size** on reliable, multi-step agentic tool use. Every MoE model tested failed at least one heavy multi-step workflow; every dense model passed. Two small dense models led the field:

| Model | Size (Q4) | Tool-call pass rate | Notes |
|---|---|---|---|
| Qwen3.5 4B | 3.4 GB | 97.5% | Beat models 5-7x its size, including an 18 GB MoE |
| Nemotron Nano 4B | 4.2 GB | 95.0% | 78% HumanEval — strong raw coding score too |

For comparison, several much larger and more heavily-marketed 2026 releases are **not** a good fit for a laptop-class machine, despite headline "active parameter" counts that sound small: a Mixture-of-Experts model's *total* weights still have to sit in memory even if only a fraction compute per token. A widely-discussed ~80B-total-parameter coding MoE needs on the order of **45-50GB of RAM even at 4-bit quantization** — completely impractical on a 16GB machine, and the tool-calling reliability data above suggests it wouldn't necessarily be a better choice even if you had the RAM.

## Recommendation

- **Start with a small dense model in the 4B-8B range.** `qwen3:4b` (what nativestudio ships a custom Modelfile for, `nativestudio:coder`) or `qwen3.5:4b` are good defaults on 16GB machines.
- **Prefer dense over MoE** for the agent/chat model specifically, even if an MoE model's benchmark scores look better on paper. This app's whole design (deterministic tools, structured patch review, verified-fact gating) exists to compensate for a small model's limitations — but it can't compensate for a model that fails to call the right tool in the first place.
- **`nomic-embed-text`** for the embedding model (semantic knowledge search) — small, fast, and what the indexer is tuned against.
- If you have more RAM/VRAM to spare, larger dense models (Qwen3 8B-14B, similar-tier Llama/Mistral models) generally do better on both raw coding quality and tool-calling — the tradeoff is speed and memory, not correctness.

### If you have room for an 8B model: IBM Granite 4.1 8B

Dense (not MoE), Apache 2.0, and matches or beats IBM's own previous 32B MoE model on most benchmarks (87.2% HumanEval) at roughly a quarter the parameters. It fits this guide's own criteria — dense architecture, permissive license, strong coding score — better than most alternatives at this size. The one thing not yet verified for it specifically is dedicated multi-step *tool-calling* reliability (the Qwen3.5 4B / Nemotron Nano 4B numbers above come from a benchmark built for exactly that; Granite's numbers are general coding/reasoning benchmarks). Worth trying if you have the RAM for an 8B model, but test it against nativestudio's actual tools before trusting it over a model with a proven tool-calling track record.

### A note on "small but as capable as Claude"

No open, laptop-sized model is that — model quality genuinely scales with training compute and parameter count, and nothing in the 4B-8B range closes that gap, no matter how good its benchmark scores look. What these models *can* do is be reliable within a narrow, well-supported task (calling the right tool, editing the right file) when the surrounding harness — this app's structured tools, patch review, and verified memory — carries the rest of the weight. That division of labor, not a bigger model, is what actually makes a small local model usable for real work.

## A caveat, and how to actually decide

The tool-calling numbers above come from a *generic* API tool-calling benchmark (weather lookups, currency conversion), not from coding-agent-specific tasks — they're a strong signal, not a guarantee that any given model will behave well on *this* app's specific tools and prompts. Model behavior on small local models can also be surprisingly inconsistent between models that look similar on paper: two similarly-sized models from the same family have been observed, in this project's own testing, to behave very differently on messy or mixed-language prompts.

**Before committing to a model change, test it live** against the actual tools you care about — a rename, a multi-file edit, a `run_terminal` call — rather than trusting a benchmark table alone. Use the model selector dropdown in the chat panel to switch mid-session and compare.
