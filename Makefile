# Makefile para mi-cli
# Uso: make <target>
# Ejemplos: make build, make test, make lint, make release

.PHONY: help build test lint fmt vet tidy install clean release snapshot check ship

# Variables
BINARY_NAME := ayrton
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Default target
help:
	@echo "Targets disponibles:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

build: ## Compila el binario para la plataforma actual
	@echo "🔨 Building $(BINARY_NAME)..."
	@go build -ldflags="$(LDFLAGS)" -o $(BINARY_NAME) .

install: build ## Instala el binario en GOPATH/bin
	@echo "📦 Installing $(BINARY_NAME)..."
	@go install -ldflags="$(LDFLAGS)" .

test: ## Ejecuta tests con coverage
	@echo "🧪 Running tests..."
	@go test -v -race -coverprofile=coverage.out ./...

test-short: ## Tests rápidos sin race detector
	@go test -short ./...

cover: test ## Abre reporte de coverage en browser
	@go tool cover -html=coverage.out

lint: ## Ejecuta linters (golangci-lint)
	@echo "🔍 Linting..."
	@golangci-lint run ./...

fmt: ## Formatea código con gofmt
	@echo "🎨 Formatting..."
	@gofmt -s -w .

vet: ## Ejecuta go vet
	@go vet ./...

tidy: ## Limpia y ordena dependencias
	@go mod tidy

generate: ## Genera código (mocks, etc.)
	@go generate ./...

clean: ## Limpia artefactos de build
	@echo "🧹 Cleaning..."
	@rm -f $(BINARY_NAME) coverage.out
	@rm -rf dist/

snapshot: ## Build snapshot con goreleaser (sin publicar)
	@echo "📸 Creating snapshot..."
	@goreleaser release --snapshot --clean

release: ## Release completo con goreleaser (requiere tag)
	@echo "🚀 Releasing..."
	@goreleaser release --clean

# Release automation
version: ## Muestra próxima versión (patch bump)
	@git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' | awk -F. '{print "v" $$1 "." $$2 "." $$3+1}' || echo "v0.1.0"

ship: check ## 🚀 Pipeline completo: validate → tag → push → release
	@echo "🚀 Shipping..."
	@VERSION=$$(make -s version); \
	echo "📦 Version: $$VERSION"; \
	git tag $$VERSION; \
	git push origin $$VERSION; \
	git push origin main; \
	echo "✅ Shipped $$VERSION"

ship-dry: check ## 🧪 Dry-run: muestra qué haria ship sin ejecutar
	@VERSION=$$(make -s version); \
	echo "📦 Would tag: $$VERSION"; \
	echo "📤 Would push: origin main + origin $$VERSION"; \
	echo "✅ Dry-run complete - no changes made"

ship-minor: check ## Ship con version minor bump
	@VERSION=$$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' | awk -F. '{print "v" $$1 "." $$2+1 ".0"}'); \
	echo "📦 Version: $$VERSION"; \
	git tag $$VERSION; \
	git push origin $$VERSION; \
	git push origin main; \
	echo "✅ Shipped $$VERSION"

ship-major: check ## Ship con version major bump
	@VERSION=$$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' | awk -F. '{print "v" $$1+1 ".0.0"}'); \
	echo "📦 Version: $$VERSION"; \
	git tag $$VERSION; \
	git push origin $$VERSION; \
	git push origin main; \
	echo "✅ Shipped $$VERSION"

check: fmt vet lint test-short ## Pipeline completo de validación local

# Development helpers
dev: build ## Build y ejecuta el CLI
	@./$(BINARY_NAME) $(ARGS)

run: ## Ejecuta directamente con go run
	@go run -ldflags="$(LDFLAGS)" . $(ARGS)

# Cross-compilation helpers
build-all: ## Compila para todas las plataformas soportadas
	@echo "🌍 Building for all platforms..."
	@GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-amd64 .
	@GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-arm64 .
	@GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-darwin-amd64 .
	@GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-darwin-arm64 .
	@GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/$(BINARY_NAME)-windows-amd64.exe .
	@ls -la dist/