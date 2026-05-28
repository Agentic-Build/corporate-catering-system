FROM golang:1.26.3-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY services/api ./services/api
# BuildKit cache mounts persist Go's module + build caches across runs (and
# end up in the GHA cache via cache-to=gha,mode=max), so only changed packages
# recompile — api build drops from ~3min cold to ~30-60s incremental.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tbite ./services/api/cmd/tbite

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/tbite /usr/local/bin/tbite
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/tbite"]
CMD ["--role=api"]
