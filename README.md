# Ayrton CLI

> **Startup in a CLI** — Autonomous agents with persistent memory for revenue generation.

---

## Why Ayrton?

Most AI coding tools are ephemeral — they forget everything between sessions. You explain your architecture, your patterns, your hard-won lessons... and next time, you start from zero.

**Ayrton changes that.**

It gives you **persistent, cross-session memory** via Engram (SQLite + FTS5) so your AI agents actually learn from your work. Combined with a full **Spec-Driven Development (SDD) workflow**, it turns AI from a clever autocomplete into a reliable engineering partner that remembers your decisions, your patterns, and your mistakes.

Built because I was tired of re-explaining the same things to every new AI session.

---

## Quick Start

```bash
# Install (via Go)
go install github.com/mauroociappinaph/ayrton@latest

# Or build from source
git clone https://github.com/mauroociappinaph/ayrton
cd ayrton && go build -o ayrton .
```

---

## Commands

| Command | Description |
|---------|-------------|
| `ayrton sdd propose/spec/design/tasks/apply/verify/archive` | Spec-Driven Development workflow |
| `ayrton learn add/recall/recent` | Learning Agent with persistent memory |
| `ayrton version` | Show version info |

---

## Learning Agent — Your AI's Long-Term Memory

Persists patterns cross-session using **Engram** (SQLite + FTS5) at `~/.ayrton/engram.db`:

```bash
# Learn a pattern
ayrton learn add "Use FTS5 for semantic search" --category architecture --confidence 0.95

# Recall patterns
ayrton learn recall "FTS5"
ayrton learn recent
```

The Learning Agent stores:
- **Pattern description** — what you learned
- **Category** — architecture, error-resolution, sdd-decision, etc.
- **Context** — why it mattered, what files involved
- **Outcome** — what happened when you applied it
- **Confidence** — how sure you are (0.0–1.0)

---

## SDD Autonomous Loop — From Issue to Implementation

Create a GitHub issue with label `autonomous` → full SDD loop executes automatically:

1. **Propose** → `.atl/proposals/{issue}.md`
2. **Spec** → `.atl/specs/{issue}.md`
3. **Design** → `.atl/designs/{issue}.md`
4. **Tasks** → `.atl/tasks/{issue}.md`
5. **Apply** → Requires AI agent (manual step)
6. **Verify** → `go test -v -race ./...`
7. **Archive** → Sync delta specs

This isn't a toy — it's how I build features in my own projects. The loop handles the boring spec/design/tasks boilerplate so you can focus on the actual implementation.

---

## Architecture

- **Core**: Go 1.23 + Cobra + Viper
- **Memory**: Engram (SQLite + FTS5) — zero CGO, fully portable
- **Agents**: 12 specialized agents (SDD phases + Learning + Auditor + Revenue + Orchestrator)
- **CI/CD**: GitHub Actions with SDD Autonomous Loop
- **Release**: GoReleaser for cross-platform binaries

---

## Installation

### Pre-built Binaries
Download from [GitHub Releases](https://github.com/mauroociappinaph/ayrton/releases) — Linux, macOS, Windows.

### From Source
```bash
git clone https://github.com/mauroociappinaph/ayrton
cd ayrton
make ship        # validates, tags, pushes, releases (see Makefile)
# or just:
go build -o ayrton .
```

---

## Development

```bash
make check        # fmt + vet + lint + test-short
make test         # full test suite with race detector
make snapshot     # local GoReleaser snapshot build
```

---

## Links

- **Repo**: https://github.com/mauroociappinaph/ayrton
- **Releases**: https://github.com/mauroociappinaph/ayrton/releases
- **Issues**: https://github.com/mauroociappinaph/ayrton/issues
- **Actions**: https://github.com/mauroociappinaph/ayrton/actions

---

## License

MIT — see [LICENSE](LICENSE) for details.