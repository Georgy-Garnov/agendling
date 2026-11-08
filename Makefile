# Agendling build targets. Run `make help` for a summary.

APP      := agendling
PKG      := github.com/Georgy-Garnov/agendling
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDVER    := -X $(PKG)/internal/appinfo.Version=$(VERSION)
# Numeric Windows file/product version from the tag: v1.2.3[-n-gHASH] -> 1.2.3.0, otherwise 0.0.0.0.
WINVER   := $(or $(shell echo "$(VERSION)" | sed -nE 's/^v?([0-9]+)\.([0-9]+)\.([0-9]+).*/\1.\2.\3.0/p'),0.0.0.0)
WINRES   := go run github.com/tc-hib/go-winres@v0.3.3
NFPM     := go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.48.0
# Oldest glibc the Linux binary may require: Ubuntu 22.04 ships 2.35.
MAXGLIBC := 2.35

WINBIN   := bin/$(APP).exe
LINUXBIN := bin/$(APP)-linux-amd64

.PHONY: help build windows linux deb glibc-check winres uishots demo test vet notices clean

help: ## Show this help
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

build: windows linux ## Build both binaries

windows: winres ## Build bin/agendling.exe (cross-compiles anywhere; no CGO)
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w $(LDVER)" -o $(WINBIN) ./cmd/$(APP)

linux: ## Build bin/agendling-linux-amd64 (on Linux; needs pkg-config and libgtk-3-dev)
	CGO_ENABLED=1 go build -trimpath -ldflags "-s -w $(LDVER)" -o $(LINUXBIN) ./cmd/$(APP)

deb: linux glibc-check ## Build dist/agendling_<version>_amd64.deb from the Linux binary (needs bin/ from `make linux`)
	mkdir -p dist
	VERSION=$(VERSION) $(NFPM) package --config packaging/nfpm.yaml --packager deb --target dist/

# The binary links against the build host's glibc; refuse builds newer distros would need.
glibc-check: ## Fail if the Linux binary needs a glibc newer than MAXGLIBC
	@need=$$(objdump -T $(LINUXBIN) | grep -o 'GLIBC_[0-9.]*' | sed 's/GLIBC_//' | sort -uV | tail -1); \
	if [ "$$(printf '%s\n%s\n' "$$need" "$(MAXGLIBC)" | sort -V | tail -1)" != "$(MAXGLIBC)" ]; then \
		echo "$(LINUXBIN) needs glibc $$need, newer than $(MAXGLIBC)"; exit 1; \
	else echo "$(LINUXBIN) needs glibc $$need (max $(MAXGLIBC))"; fi

# Icon, manifest (Common Controls v6, per-monitor DPI) and version info for the Windows exe.
winres: ## Generate Windows resources (icon, manifest, version info) from cmd/agendling/winres
	$(WINRES) make --in cmd/$(APP)/winres/winres.json --out cmd/$(APP)/rsrc --arch amd64 \
		--product-version $(WINVER) --file-version $(WINVER)

# Screenshot tour of the Windows UI with demo data; run bin/uishots.exe DIR on Windows.
uishots: ## Build bin/uishots.exe (Windows UI screenshot tour, with the app's manifest)
	$(WINRES) make --in cmd/$(APP)/winres/winres.json --out tools/uishots/rsrc --arch amd64
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags uitour -o bin/uishots.exe ./tools/uishots

demo: ## Build bin/agendling-demo.exe: the Windows app on demo calendars, away from real data
	$(WINRES) make --in cmd/$(APP)/winres/winres.json --out tools/demoapp/rsrc --arch amd64
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags uitour -ldflags "-H windowsgui $(LDVER)" -o bin/agendling-demo.exe ./tools/demoapp

test: ## Run unit and integration tests
	go test ./internal/...

vet: ## Vet both platforms
	GOOS=windows go vet ./...
	go vet ./...

notices: ## Regenerate THIRD_PARTY_NOTICES.md
	go run ./tools/notices > THIRD_PARTY_NOTICES.md

clean: ## Remove build output
	rm -rf bin dist
