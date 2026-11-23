# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bmd ./cmd/bmd \
 && mkdir -p /data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /data /data
COPY --from=build /bmd /bmd
USER 65532:65532
WORKDIR /data
VOLUME /data
ENTRYPOINT ["/bmd"]
CMD ["--help"]
