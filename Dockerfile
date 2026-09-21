# Build stage.
FROM golang:1.25-alpine AS build

WORKDIR /src

# Copy the module files first so dependency download is cached independently of
# the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
# CGO_ENABLED=0 keeps the "single static binary, no CGO toolchain" promise: the
# SQLite driver is pure Go.
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/x-cyber-cover-hub \
      ./cmd/server

# Runtime stage.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 app \
 && mkdir -p /data \
 && chown app:app /data

COPY --from=build /out/x-cyber-cover-hub /x-cyber-cover-hub

USER app
WORKDIR /
VOLUME ["/data"]

EXPOSE 3400

ENTRYPOINT ["/x-cyber-cover-hub"]
CMD ["-cache", "/data/covers.db"]
