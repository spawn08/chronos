# ── Build stage ──────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

ARG VERSION=dev
ARG COMMIT=unknown

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build \
    -ldflags "-s -w \
      -X github.com/chronos-ai/chronos/cli/cmd.Version=${VERSION} \
      -X github.com/chronos-ai/chronos/cli/cmd.Commit=${COMMIT}" \
    -o /chronos ./cli/main.go

# ── Runtime stage ────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache sqlite-libs ca-certificates tzdata

COPY --from=builder /chronos /usr/local/bin/chronos

RUN addgroup -S chronos && adduser -S chronos -G chronos
USER chronos

EXPOSE 8420

ENTRYPOINT ["chronos"]
CMD ["serve", ":8420"]
