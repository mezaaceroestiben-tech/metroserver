# Generado por Mega panel de GitHub: servidor de Escuchar juntos con lista de apps permitidas.
FROM golang:1.25.6-alpine AS builder
RUN apk add --no-cache git protobuf-dev bash
WORKDIR /build
COPY go.mod go.sum ./
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 && go mod download
ENV PATH="/root/go/bin:${PATH}"
COPY . .
# Si el submódulo del protocolo no vino con el código, se descarga aquí.
RUN [ -f metroproto/listentogether.proto ] || (rm -rf metroproto && git clone --depth 1 https://github.com/metrolistgroup/metroproto metroproto)
RUN bash scripts/generate_proto.sh
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o metroserver -ldflags="-w -s" ./cmd/metroserver

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
RUN addgroup -g 1000 metrolist && adduser -D -u 1000 -G metrolist metrolist
WORKDIR /app
COPY --from=builder /build/metroserver .
COPY ua_policy.json /app/ua_policy.json
RUN mkdir -p /app/data && chown -R metrolist:metrolist /app
ENV UA_POLICY_FILE=/app/ua_policy.json
USER metrolist
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider "http://localhost:${PORT:-8080}/health" || exit 1
CMD ["./metroserver"]
