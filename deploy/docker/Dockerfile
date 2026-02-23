FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /chronos ./cli/main.go

FROM alpine:3.20
RUN apk add --no-cache sqlite-libs ca-certificates
COPY --from=builder /chronos /usr/local/bin/chronos
EXPOSE 8420
ENTRYPOINT ["chronos"]
CMD ["serve"]
