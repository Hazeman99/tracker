# syntax=docker/dockerfile:1

# ---- build -------------------------------------------------------------
FROM golang:1.27.4-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN go build -ldflags="-s -w" -o /out/tracker-api ./cmd/tracker-api \
 && mkdir -p /out/data

# ---- runtime -----------------------------------------------------------
# Static distroless: no shell, no package manager, runs as uid 65532.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/tracker-api /tracker-api
# Docker seeds a new named volume from this directory, owner-only.
COPY --from=build --chown=65532:65532 --chmod=0700 /out/data /data
USER 65532:65532
ENV TRACKER_LISTEN_ADDR=:8080 TRACKER_DB_PATH=/data/tracker.db
EXPOSE 8080
ENTRYPOINT ["/tracker-api"]
CMD ["serve"]
