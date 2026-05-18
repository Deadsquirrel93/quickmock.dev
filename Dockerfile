# syntax=docker/dockerfile:1.7

# ---------- build stage ----------
FROM golang:1.22-alpine AS build

# curl is needed by scripts/vendor-js.sh so we can embed HTMX + Alpine.
RUN apk add --no-cache curl bash

WORKDIR /src

# Cache deps separately from source for faster rebuilds.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

# Make sure vendor JS exists in the image even if the developer forgot to
# run `make vendor-js` locally. Re-runs are idempotent and cached.
RUN ./scripts/vendor-js.sh

# Static binary, stripped, reproducible-ish.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/quickmock ./cmd/server

# ---------- runtime stage ----------
# distroless/static: ~2 MB, no shell, no package manager, no CVE noise.
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Bring in the binary and the assets it expects to find on disk.
# Templates and locales are embedded via embed.FS at build time, so they
# do not need to be copied here. The migrations dir is also embedded.
COPY --from=build /out/quickmock /app/quickmock

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/app/quickmock"]
CMD []
