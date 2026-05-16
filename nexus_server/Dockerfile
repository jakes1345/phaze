FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/nexus-server .

# Pull a prebuilt litestream binary. Pinned to a specific release for
# reproducibility; bump the version in lockstep with the Go upgrade cadence.
FROM alpine:3.20 AS litestream-dl
ARG LITESTREAM_VERSION=0.3.13
RUN apk add --no-cache curl tar \
 && curl -sL "https://github.com/benbjohnson/litestream/releases/download/v${LITESTREAM_VERSION}/litestream-v${LITESTREAM_VERSION}-linux-amd64.tar.gz" \
    | tar -xz -C /usr/local/bin litestream

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/nexus-server /app/nexus-server
COPY --from=litestream-dl /usr/local/bin/litestream /usr/local/bin/litestream
COPY templates /app/templates
COPY public /app/public
COPY litestream.yml /app/litestream.yml
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

ENV PORT=8080
ENV DB_PATH=/data/nexus.db
EXPOSE 8080

CMD ["/app/entrypoint.sh"]
