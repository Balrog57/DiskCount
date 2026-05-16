FROM golang:1.26-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /diskcount ./cmd/diskcount/

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata && adduser -D diskcount

USER diskcount
WORKDIR /app

COPY --from=builder /diskcount /app/diskcount

EXPOSE 47832

CMD ["/app/diskcount"]
