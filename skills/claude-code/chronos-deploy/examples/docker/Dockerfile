FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o chronos ./cli/main.go

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /app/chronos /usr/local/bin/chronos
COPY agents.yaml /etc/chronos/agents.yaml
EXPOSE 8420
HEALTHCHECK --interval=30s --timeout=10s --retries=3 \
  CMD wget -q --spider http://localhost:8420/health || exit 1
ENTRYPOINT ["chronos"]
CMD ["serve", ":8420"]
