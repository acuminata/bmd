FROM golang:1.27-alpine AS build
RUN apk add --no-cache ca-certificates

WORKDIR /src

mkdir -p /data

# Cache Go dependencies
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bmd ./cmd/bmd


FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /data /data
COPY --from=build /bmd /bmd
USER 65532:65532

WORKDIR /data

VOLUME /data

ENTRYPOINT ["/bmd"]
CMD ["--help"]
