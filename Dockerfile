FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY web ./web

ARG TARGETOS=linux
ARG TARGETARCH=arm64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/fridge-flow-web ./cmd/web

FROM alpine:3.21
WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S app -G app

COPY --from=builder /out/fridge-flow-web /app/fridge-flow-web
COPY --from=builder /src/internal/db/migrations /app/internal/db/migrations
COPY --from=builder /src/web/templates /app/web/templates
COPY --from=builder /src/web/static /app/web/static

ENV APP_PORT=8080
ENV MIGRATIONS_DIR=/app/internal/db/migrations
ENV TEMPLATE_DIR=/app/web/templates
ENV STATIC_DIR=/app/web/static

USER app
EXPOSE 8080
CMD ["/app/fridge-flow-web"]
