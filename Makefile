SHELL := /bin/bash

GO       ?= go
NPM      ?= npm
BIN      ?= steelpage-desktop
FRONTEND := frontend

.PHONY: all build build-frontend build-backend run dev dev-backend dev-frontend test clean tidy

all: build

build: build-frontend build-backend

build-frontend:
	cd $(FRONTEND) && $(NPM) install --no-audit --no-fund
	cd $(FRONTEND) && $(NPM) run build

build-backend:
	$(GO) build -o $(BIN) ./cmd/steelpage-desktop

run: build
	./$(BIN)

# Frontend dev loop: Vite serves the SPA on :5173 and proxies /api + /docs
# to the headless backend on :18080. Run both targets in separate terminals
# and open http://localhost:5173 in a browser.
dev:
	@echo "Run these in separate terminals:"
	@echo "  make dev-backend   # headless API on 127.0.0.1:18080"
	@echo "  make dev-frontend  # Vite on http://localhost:5173"

dev-backend:
	$(GO) run ./cmd/steelpage-desktop -headless -bind 127.0.0.1:18080

dev-frontend:
	cd $(FRONTEND) && $(NPM) run dev

test:
	$(GO) test ./...
	cd $(FRONTEND) && $(NPM) run check

tidy:
	$(GO) mod tidy

clean:
	rm -f $(BIN)
	rm -rf $(FRONTEND)/dist
	mkdir -p $(FRONTEND)/dist
	touch $(FRONTEND)/dist/.gitkeep
