FROM golang:1.26.5-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./

RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /out/browser-stream ./cmd/server

FROM debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive
ENV DISPLAY=:99
ENV LANG=C.UTF-8
ENV LC_ALL=C.UTF-8

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && curl -fsSLo /usr/share/keyrings/brave-browser-archive-keyring.gpg \
      https://brave-browser-apt-release.s3.brave.com/brave-browser-archive-keyring.gpg \
    && curl -fsSLo /etc/apt/sources.list.d/brave-browser-release.sources \
      https://brave-browser-apt-release.s3.brave.com/brave-browser.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
    brave-browser \
    ffmpeg \
    xvfb \
    x11-utils \
    xdotool \
    jq \
    fonts-liberation \
    fonts-noto-cjk \
    pulseaudio \
    pulseaudio-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=build /out/browser-stream /app/browser-stream
COPY web /app/web
COPY scripts/brave.sh scripts/widevine.sh /usr/local/lib/browser-stream/
COPY start.sh /start.sh

RUN chmod +x /start.sh

EXPOSE 8080
EXPOSE 50000-50010/udp

ENTRYPOINT ["/start.sh"]
