FROM golang:1.26.3-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY services/api ./services/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/tbite ./services/api/cmd/tbite

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/tbite /usr/local/bin/tbite
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/tbite"]
CMD ["--role=api"]
