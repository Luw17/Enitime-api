FROM golang:1.27-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /api ./cmd/api

FROM alpine:3.22

WORKDIR /app
COPY --from=builder /api ./api
EXPOSE 8080
ENTRYPOINT ["./api"]
