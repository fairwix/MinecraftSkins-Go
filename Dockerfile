FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.22 AS goose
ARG TARGETOS
ARG TARGETARCH
RUN apk add --no-cache ca-certificates \
    && case "$TARGETARCH" in \
        amd64) release_arch=x86_64 ;; \
        arm64) release_arch=arm64 ;; \
        *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && wget -q -O /usr/local/bin/goose "https://github.com/pressly/goose/releases/download/v3.28.0/goose_${TARGETOS}_${release_arch}" \
    && chmod 0755 /usr/local/bin/goose
ENTRYPOINT ["/usr/local/bin/goose"]

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/api /app/api
USER app
EXPOSE 8080
ENTRYPOINT ["/app/api"]
