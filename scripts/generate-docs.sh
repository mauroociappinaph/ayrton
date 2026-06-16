#!/bin/bash
set -e

echo "📝 Generating documentation..."

# 1. Generate README from template + current state
cat > README.md << 'READMEEOF'
# Ayrton CLI

> **Startup in a CLI** - Autonomous agents with persistent memory for revenue generation.

## 🚀 Quick Start

```bash
# Install
go install github.com/mauroociappinaph/ayrton@latest

# Or build from source
git clone https://github.com/mauroociappinaph/ayrton
cd ayrton && go build -o ayrton .
```

## 🤖 Commands

| Command | Description |
|---------|-------------|
| `ayrton sdd propose/spec/design/tasks/apply/verify/archive` | Spec-Driven Development workflow |
| `ayrton learn add/recall/recent` | Learning Agent with persistent memory |
| `ayrton version` | Show version info |

## 🧠 Learning Agent

Persists patterns cross-session using Engram (SQLite + FTS5):

```bash
# Learn a pattern
ayrton learn add "Use FTS5 for semantic search" --category architecture --confidence 0.95

# Recall patterns
ayrton learn recall "FTS5"
ayrton learn recent
```

## 🔄 SDD Autonomous Loop

Create a GitHub issue with label `autonomous` → full SDD loop executes automatically:

1. **Propose** → `.atl/proposals/{issue}.md`
2. **Spec** → `.atl/specs/{issue}.md`
3. **Design** → `.atl/designs/{issue}.md`
4. **Tasks** → `.atl/tasks/{issue}.md`
5. **Apply** → Requires AI agent (manual)
6. **Verify** → `go test -v -race ./...`
7. **Archive** → Sync delta specs

## 🏗️ Architecture

- **Core**: Go 1.25 + Cobra + Viper
- **Memory**: Engram (SQLite + FTS5) at `~/.ayrton/engram.db`
- **Agents**: 12 agents defined (SDD phases + Learning + Auditor + Revenue + Orchestrator)
- **CI/CD**: GitHub Actions with SDD Autonomous Loop

## 🔗 Links

- **Repo**: https://github.com/mauroociappinaph/ayrton
- **Issues**: https://github.com/mauroociappinaph/ayrton/issues
- **Actions**: https://github.com/mauroociappinaph/ayrton/actions

---

*Generated automatically by Ayrton SDD Autonomous Loop*
READMEEOF

echo "✅ README.md generated"

# 2. Generate CHANGELOG from git tags
if git describe --tags --abbrev=0 2>/dev/null; then
    LAST_TAG=$(git describe --tags --abbrev=0)
    git log --pretty=format:"- %s (%h)" $LAST_TAG..HEAD > CHANGELOG.md 2>/dev/null || echo "# Changelog\n\nNo changes since $LAST_TAG" > CHANGELOG.md
else
    git log --pretty=format:"- %s (%h)" -20 > CHANGELOG.md
fi

echo "✅ CHANGELOG.md generated"

# 3. Generate API docs (if any Go packages)
if [ -d "cmd" ] || [ -d "internal" ]; then
    echo "# API Reference" > API.md
    echo "" >> API.md
    echo "Generated on $(date)" >> API.md
    echo "" >> API.md
    for f in $(find cmd internal -name "*.go" -not -name "*_test.go" 2>/dev/null | head -20); do
        echo "## \`$f\`" >> API.md
        grep -E "^func |^type " "$f" | head -10 | sed 's/^/- /' >> API.md
        echo "" >> API.md
    done
    echo "✅ API.md generated"
fi

echo "🎉 Documentation generation complete!"
