FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /coord ./cmd/coord

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /coord /usr/local/bin/coord

EXPOSE 8080/tcp
EXPOSE 3478/udp
EXPOSE 3479/udp

ENTRYPOINT ["/usr/local/bin/coord"]
