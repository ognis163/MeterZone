# MeterZone в контейнере.
# Сборка идёт внутри образа, поэтому подойдёт любой NAS: arm64 и x86_64.

# ---------- сборка ----------
FROM golang:1.23-alpine AS build

WORKDIR /src

# Статический бинарник: без CGO, без внешних библиотек.
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOFLAGS=-trimpath

# Сначала зависимости — этот слой кэшируется и не пересобирается
# при изменении исходников. third_party нужен из-за replace в go.mod.
COPY go.mod go.sum ./
COPY third_party ./third_party
RUN go mod download

COPY . .
RUN go build -ldflags "-s -w" -o /out/meterzone .

# ---------- запуск ----------
FROM alpine:3.20

# ca-certificates — для SMTP через SSL/STARTTLS.
# tzdata — на случай APP_TZ (в бинарник тоже вшит, но так надёжнее).
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=build /out/meterzone /app/meterzone

# Данные: база и резервные копии — в одном томе /data.
ENV HOST=0.0.0.0 \
    PORT=8099 \
    APP_DB=/data/meterzone.db \
    APP_BACKUP_DIR=/data/backups \
    PORT_FIXED=1

VOLUME ["/data"]
EXPOSE 8099

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8099/healthz || exit 1

ENTRYPOINT ["/app/meterzone"]
