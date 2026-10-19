# Сборка и установка MeterZone на Keenetic Hopper (Entware, aarch64)

Приложение — один статический бинарник. На роутере не нужны Go, Node или база данных:
SQLite и шрифт встроены.

## 1. Сборка на ПК

```powershell
$env:GOOS='linux'; $env:GOARCH='arm64'; $env:CGO_ENABLED='0'
go build -trimpath -ldflags "-s -w" -o meterzone .
```

## 2. Копирование на роутер

Включите Entware и SSH на роутере. Дальше — любой удобный способ передачи.

По SSH, если на компьютере есть pscp (PuTTY):

```powershell
pscp -pw <пароль> .\meterzone deep@<адрес роутера>:/tmp/meterzone
```

Или через HTTP-сервер в вашей сети:

```sh
curl -fSL http://<адрес компьютера>:18080/meterzone -o /tmp/meterzone
```

## 3. Установка

```sh
mkdir -p /opt/meterzone/data
cp /tmp/meterzone /opt/meterzone/meterzone
chmod 755 /opt/meterzone/meterzone
```

Скрипт автозапуска лежит в репозитории: `deploy/S99meterzone`.
Скопируйте его в `/opt/meterzone/S99meterzone` и в `/opt/etc/init.d/S99meterzone`.

```sh
chmod 755 /opt/meterzone/S99meterzone /opt/etc/init.d/S99meterzone
```

## 4. Запуск

```sh
/opt/etc/init.d/S99meterzone start
/opt/etc/init.d/S99meterzone status
tail -n 30 /opt/var/log/meterzone.log
```

Откройте в браузере телефона или ПК: **http://<адрес роутера>:8099**

## 5. Обновление

```sh
/opt/etc/init.d/S99meterzone stop
cp /tmp/meterzone /opt/meterzone/meterzone
chmod 755 /opt/meterzone/meterzone
/opt/etc/init.d/S99meterzone start
```

База в `/opt/meterzone/data/meterzone.db` при обновлении не затрагивается.
Перед обновлением можно сохранить её копию в `/opt/backup/`.

## 6. Где что лежит

| Что | Где |
|---|---|
| Программа | `/opt/meterzone/meterzone` |
| База SQLite | `/opt/meterzone/data/meterzone.db` |
| Резервные копии | `/opt/meterzone/data/backups/` (14 последних) |
| Лог | `/opt/var/log/meterzone.log` |
| Автозапуск | `/opt/etc/init.d/S99meterzone` |
| Порт | 8099 |

## 7. Диагностика

```sh
/opt/etc/init.d/S99meterzone status
ps | grep meterzone | grep -v grep
tail -n 30 /opt/var/log/meterzone.log
```

Если порт занят, задайте другой в `S99meterzone` (переменная `PORT`) или в настройках приложения.

## 8. Переменные окружения

| Переменная | По умолчанию | Что задаёт |
|---|---|---|
| `PORT` | 8099 | порт HTTP |
| `HOST` | 0.0.0.0 | адрес прослушивания |
| `APP_DB` | `data/meterzone.db` рядом с бинарником | файл базы |
| `APP_BACKUP_DIR` | `data/backups` | каталог резервных копий |
| `APP_TZ` | системный | часовой пояс, например `Europe/Moscow` |
