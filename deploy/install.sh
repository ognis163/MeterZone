#!/bin/sh
# MeterZone — установка на Keenetic / Entware (aarch64) одной командой.
# Запуск: curl -fsSL https://raw.githubusercontent.com/ognis163/MeterZone/main/deploy/install.sh | sh
set -eu

APP_DIR=/opt/meterzone
BIN="$APP_DIR/meterzone"
INIT="$APP_DIR/S99meterzone"
PORT=${PORT:-8099}
REPO=ognis163/MeterZone
ASSET=meterzone-linux-arm64
BASE="https://github.com/$REPO/releases/latest/download"
TMP="$APP_DIR/.meterzone.$$"

say() { printf '%s\n' "$*"; }
die() { printf 'Ошибка: %s\n' "$*" >&2; exit 1; }

cleanup() { rm -f "$TMP" "$TMP.bin" 2>/dev/null || true; }
trap cleanup EXIT HUP INT TERM

[ "$(id -u)" = "0" ] || die "запустите от root"
[ -d /opt ] || die "не найден /opt — сначала установите Entware"
command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 || die "нужен curl или wget"

ARCH=$(uname -m)
case "$ARCH" in
  aarch64|arm64) ;;
  *) die "поддерживается только aarch64 (Keenetic Hopper), у вас: $ARCH" ;;
esac

fetch() {
  url=$1
  out=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  else
    wget -qO "$out" "$url"
  fi
}

mkdir -p "$APP_DIR/data" "$APP_DIR/data/backups"

say "Скачиваю MeterZone (aarch64)..."
fetch "$BASE/$ASSET" "$TMP.bin" || die "не удалось скачать $BASE/$ASSET"
[ -s "$TMP.bin" ] || die "скачался пустой файл"
MAGIC=$(dd if="$TMP.bin" bs=1 count=4 2>/dev/null | od -An -tx1 | tr -d ' \n')
[ "$MAGIC" = "7f454c46" ] || die "файл не похож на Linux-бинарник"

if [ -x /opt/etc/init.d/S99meterzone ]; then
  say "Останавливаю прежнюю версию..."
  /opt/etc/init.d/S99meterzone stop >/dev/null 2>&1 || true
  sleep 1
fi
[ -f "$BIN" ] && cp "$BIN" "$BIN.prev" 2>/dev/null || true

mv "$TMP.bin" "$BIN"
chmod 755 "$BIN"

say "Настраиваю автозапуск..."
cat > "$INIT" <<EOF
#!/bin/sh
# MeterZone — учёт показаний приборов учёта (Keenetic / Entware)
# Запуск: /opt/etc/init.d/S99meterzone {start|stop|restart|status}

APP_DIR=/opt/meterzone
APP_BIN=\$APP_DIR/meterzone
LOG=/opt/var/log/meterzone.log
PORT=$PORT

case "\$1" in
  start)
    if pidof meterzone >/dev/null 2>&1; then
      echo "meterzone already running"
      exit 0
    fi
    mkdir -p "\$APP_DIR/data"
    echo "Starting MeterZone on port \$PORT..."
    cd "\$APP_DIR" || exit 1
    PORT=\$PORT HOST=0.0.0.0 ZONEINFO=/opt/share/zoneinfo \\
      APP_DB=\$APP_DIR/data/meterzone.db \\
      APP_BACKUP_DIR=\$APP_DIR/data/backups \\
      "\$APP_BIN" >> "\$LOG" 2>&1 &
    sleep 2
    if pidof meterzone >/dev/null 2>&1; then echo "ok"; else echo "failed, see \$LOG"; fi
    ;;
  stop)
    echo "Stopping MeterZone..."
    kill \$(pidof meterzone) 2>/dev/null
    ;;
  restart)
    "\$0" stop
    sleep 1
    "\$0" start
    ;;
  status)
    if pidof meterzone >/dev/null 2>&1; then echo "running"; else echo "stopped"; fi
    ;;
  *)
    echo "Usage: \$0 {start|stop|restart|status}"
    exit 1
    ;;
esac
exit 0
EOF
chmod 755 "$INIT"
mkdir -p /opt/etc/init.d
cp "$INIT" /opt/etc/init.d/S99meterzone
chmod 755 /opt/etc/init.d/S99meterzone

say "Запускаю..."
/opt/etc/init.d/S99meterzone start >/dev/null 2>&1 || true

i=0
OUT=""
while [ "$i" -lt 20 ]; do
  if command -v curl >/dev/null 2>&1; then
    OUT=$(curl -fsS "http://127.0.0.1:$PORT/healthz" 2>/dev/null || true)
  else
    OUT=$(wget -qO- "http://127.0.0.1:$PORT/healthz" 2>/dev/null || true)
  fi
  [ -n "$OUT" ] && break
  i=$((i + 1))
  sleep 1
done

if [ -z "$OUT" ]; then
  say "Сервис не ответил. Последние строки лога:"
  tail -n 20 /opt/var/log/meterzone.log 2>/dev/null || true
  die "проверьте лог /opt/var/log/meterzone.log"
fi

IP=$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')
say ""
say "Готово. MeterZone установлен и запущен."
if [ -n "$IP" ]; then
  say "Откройте: http://$IP:$PORT"
else
  say "Откройте: http://<адрес роутера>:$PORT"
fi
say "База: $APP_DIR/data/meterzone.db (существующая база не удаляется)"
say "Автозапуск: /opt/etc/init.d/S99meterzone"
say "Ответ сервера: $OUT"
