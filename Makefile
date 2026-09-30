# Agendling build targets. Run `make help` for a summary.

APP      := agendling
PKG      := github.com/Georgy-Garnov/agendling
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDVER    := -X $(PKG)/internal/appinfo.Version=$(VERSION)
# Numeric Windows file/product version from the tag: v1.2.3[-n-gHASH] -> 1.2.3.0, otherwise 0.0.0.0.
WINVER   := $(or $(shell echo "$(VERSION)" | sed -nE 's/^v?([0-9]+)\.([0-9]+)\.([0-9]+).*/\1.\2.\3.0/p'),0.0.0.0)
WINRES   := go run github.com/tc-hib/go-winres@v0.3.3

WINBIN   := bin/$(APP).exe
LINUXBIN := bin/$(APP)-linux-amd64

.PHONY: help build windows linux winres test vet notices clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: windows linux ## Build both binaries

windows: winres ## Build bin/agendling.exe (cross-compiles anywhere; no CGO)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w $(LDVER)" -o $(WINBIN) ./cmd/$(APP)

linux: ## Build bin/agendling-linux-amd64 (on Linux; needs pkg-config and libgtk-3-dev)
	CGO_ENABLED=1 go build -trimpath -ldflags "-s -w $(LDVER)" -o $(LINUXBIN) ./cmd/$(APP)

# Icon, manifest (Common Controls v6, per-monitor DPI) and version info for the Windows exe.
winres: ## Generate Windows resources (icon, manifest, version info) from cmd/agendling/winres
	$(WINRES) make --in cmd/$(APP)/winres/winres.json --out cmd/$(APP)/rsrc --arch amd64 \
		--product-version $(WINVER) --file-version $(WINVER)

test: ## Run unit and integration tests
	go test ./internal/...

vet: ## Vet both platforms
	GOOS=windows go vet ./...
	go vet ./...

notices: ## Regenerate THIRD_PARTY_NOTICES.md
	go run ./tools/notices > THIRD_PARTY_NOTICES.md

clean: ## Remove build output
	rm -rf bin
