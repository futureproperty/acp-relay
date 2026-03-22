# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/acp-relay ./cmd/acp-relay/

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tini
COPY --from=builder /bin/acp-relay /usr/local/bin/acp-relay

ENTRYPOINT ["tini", "--", "acp-relay"]
CMD ["serve", "--listen", ":8080"]

EXPOSE 8080
