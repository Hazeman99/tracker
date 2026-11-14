# Tracker — root Makefile.
# Tool versions are pinned; `go run pkg@version` keeps them out of the
# modules' dependency graphs.
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@v1.9.0
GOSEC       := github.com/securego/gosec/v2/cmd/gosec@v2.30.0

DEV_DB   ?= ./dev.db
DEV_ADDR ?= 127.0.0.1:8080

# Set VULNCHECK_IN_DOCKER=1 if vuln.go.dev is unreachable from the host
# network; govulncheck then runs in a golang container (source mounted read-only).
VULNCHECK_IN_DOCKER ?= 0
GO_IMAGE := golang:1.27.4-bookworm
ifeq ($(VULNCHECK_IN_DOCKER),1)
vulncheck = docker run --rm -v "$(CURDIR)/$(1)":/src:ro -w /src -e GOFLAGS=-buildvcs=false $(GO_IMAGE) go run $(GOVULNCHECK) $(2)
else
vulncheck = cd $(1) && go run $(GOVULNCHECK) $(2)
endif

.PHONY: check api-check desktop-check api-run api-build desktop-dev desktop-build

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
