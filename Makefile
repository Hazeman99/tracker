# Tracker — root Makefile.
# Tool versions are pinned; `go run pkg@version` keeps them out of the
# modules' dependency graphs.
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0
GOSEC       := github.com/securego/gosec/v2/cmd/gosec@v2.30.0

DEV_DB   ?= ./dev.db
DEV_ADDR ?= 127.0.0.1:8080

# Set VULNCHECK_IN_DOCKER=1 if vuln.go.dev is unreachable from the host
# network; the database is then fetched through a container.
VULNCHECK_IN_DOCKER ?= 0
GO_IMAGE := golang:1.27.4-bookworm
VULNDB := $(CURDIR)/.cache/vulndb
ifeq ($(VULNCHECK_IN_DOCKER),1)
# Download the vulnerability database through a container (which may use a
# different network path), then scan locally against that copy.
vulncheck = $(MAKE) --no-print-directory vulndb && cd $(1) && go run $(GOVULNCHECK) -db file://$(VULNDB)/db $(2)
else
vulncheck = cd $(1) && go run $(GOVULNCHECK) $(2)
endif

.PHONY: vulndb
vulndb:
	mkdir -p $(VULNDB)
	docker run --rm -v "$(VULNDB)":/out $(GO_IMAGE) curl -sSfL -o /out/vulndb.zip https://vuln.go.dev/vulndb.zip
	cd $(VULNDB) && unzip -q -o vulndb.zip -d db

.PHONY: check api-check desktop-check desktop-frontend api-run api-build desktop-dev desktop-build

## check: vet, race tests, govulncheck and gosec (API and desktop Go code) and a frontend type check.
check: api-check desktop-check

api-check:
	cd api && go vet ./...
	cd api && go test -race -count=1 ./...
	$(call vulncheck,api,./...)
	cd api && go run $(GOSEC) -quiet ./...

## api-run: run the API locally (database api/dev.db, listening on 127.0.0.1:8080).
api-run:
	cd api && TRACKER_DB_PATH=$(DEV_DB) TRACKER_LISTEN_ADDR=$(DEV_ADDR) go run ./cmd/tracker-api serve

api-build:
	cd api && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/tracker-api ./cmd/tracker-api

# ---------------------------------------------------------------- desktop
# Requires the Wails CLI: go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
WAILS ?= wails3

desktop-frontend:
	cd desktop && $(WAILS) generate bindings -clean=true -ts -i
	cd desktop/frontend && npm ci && npm run build

desktop-check: desktop-frontend
	cd desktop && go vet ./... && go vet -tags dev ./...
	cd desktop && go test -race -count=1 ./... && go test -count=1 -tags dev ./internal/serverurl
	$(call vulncheck,desktop,./...)
	cd desktop && go run $(GOSEC) -quiet ./...

## desktop-dev: run the desktop app with hot reload (dev build: also accepts http://localhost servers).
desktop-dev:
	cd desktop && $(WAILS) dev -config ./build/config.yml

## desktop-build: production build for this OS (macOS: desktop/bin/tracker.app, Linux: desktop/bin/tracker).
UNAME_S := $(shell uname -s)
desktop-build:
ifeq ($(UNAME_S),Darwin)
	cd desktop && $(WAILS) package
else
	cd desktop && $(WAILS) build
endif
