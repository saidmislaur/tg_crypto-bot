FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o crypto_bot main.go

FROM alpine:3.20

WORKDIR /app

COPY --from=builder /app/crypto_bot ./crypto_bot
COPY rate_config.json ./rate_config.json

ENV RATE_CONFIG_PATH=rate_config.json
ENV PORT=8080

EXPOSE 8080

CMD ["./crypto_bot"]
