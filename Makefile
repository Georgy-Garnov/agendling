# Agendling build targets. Run `make help` for a summary.

APP      := agendling
PKG      := github.com/Georgy-Garnov/agendling
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDVER    := -X $(PKG)/internal/appinfo.Version=$(VERSION)
SYSO     := cmd/$(APP)/rsrc_windows_amd64.syso

WINBIN   := bin/$(APP).exe
LINUXBIN := bin/$(APP)-linux-amd64

.PHONY: help build windows linux test vet syso notices clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: windows linux ## Build both binaries

windows: $(SYSO) ## Build bin/agendling.exe (cross-compiles anywhere; no CGO)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w $(LDVER)" -o $(WINBIN) ./cmd/$(APP)

linux: ## Build bin/agendling-linux-amd64 (on Linux; needs pkg-config and libgtk-3-dev)
	CGO_ENABLED=1 go build -trimpath -ldflags "-s -w $(LDVER)" -o $(LINUXBIN) ./cmd/$(APP)

# The manifest enables Common Controls v6 and per-monitor DPI awareness (required by walk).
$(SYSO): cmd/$(APP)/$(APP).manifest
	go run github.com/akavel/rsrc@v0.10.2 -manifest $< -arch amd64 -o $@

syso: $(SYSO) ## Regenerate the Windows manifest resource

test: ## Run unit and integration tests
	go test ./internal/...

vet: ## Vet both platforms
	GOOS=windows go vet ./...
	go vet ./...

notices: ## Regenerate THIRD_PARTY_NOTICES.md
	go run ./tools/notices > THIRD_PARTY_NOTICES.md

clean: ## Remove build output
	rm -rf bin
