FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o crawler ./cmd/server

FROM alpine:latest
# chromium + friends: the JS-render fallback (internal/render) drives a real
# headless browser for pages a plain HTTP fetch can't see real content on
# (React/Vue/Next.js apps). Without this, the fallback silently no-ops and
# those pages are analysed from their raw (often near-empty) HTML only.
RUN apk --no-cache add ca-certificates chromium nss freetype harfbuzz ttf-freefont
WORKDIR /root/
COPY --from=builder /app/crawler .
EXPOSE 8081
CMD ["./crawler"]