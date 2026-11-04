# MeterZone на Synology DSM 7 (Container Manager)

Приложение в контейнере: база SQLite и резервные копии лежат в одной папке на NAS
и не теряются при обновлении. Отдельный пользователь в DSM не нужен.
Образ готовый — на NAS ничего не компилируется.

## Что нужно

- DSM 7.x с пакетом **Container Manager** (Центр пакетов).
- Общая папка для проекта, например `/volume1/docker/meterzone`.
- Доступ NAS к `ghcr.io` (GitHub Container Registry).

## Шаг 1. Папка проекта

В File Station создайте `docker/meterzone`. Внутри нужны два предмета:

```
docker/meterzone/
├── docker-compose.yml
└── data/          ← база и копии, создаётся один раз вручную
```

`docker-compose.yml` положите тот, что лежит в репозитории
[ognis163/meterzone-dist](https://github.com/ognis163/meterzone-dist)
(или в этом репозитории — он одинаковый).

Папку `data` **создайте заранее**: Synology не создаёт её сама, и без неё
контейнер падает с `Bind mount failed ... does not exist`.

```sh
mkdir -p /volume1/docker/meterzone/data
```

## Шаг 2. Запуск

Container Manager → **Проект** → **Создать**:

| Поле | Значение |
|---|---|
| Имя проекта | `meterzone` |
| Путь | `/volume1/docker/meterzone` |
| Источник | существующий `docker-compose.yml` |

Или из SSH — то же самое:

```sh
cd /volume1/docker/meterzone
sudo docker compose up -d
```

Container Manager скачает образ `ghcr.io/ognis163/meterzone-dist:latest`
и запустит контейнер.

## Шаг 3. Проверка

Откройте **http://адрес-NAS:8099**. Версию можно спросить напрямую:

```sh
curl -s http://127.0.0.1:8099/healthz
# {"db":"/data/meterzone.db","ok":true,"uptime":"4s","version":"1.5.7"}
```

Если страница не открылась — в Container Manager откройте проект `meterzone`
и посмотрите **Журнал** контейнера. Приложение печатает при старте:

```
MeterZone 1.5.7 · Europe/Astrakhan
слушаю порт 8099
база: /data/meterzone.db
резервные копии: /data/backups
сервер запущен на http://192.168.x.x:8099
```

## Обновление

База в `data/` при обновлении не затрагивается — меняется только образ.
Каждая версия лежит под своим тегом: `1.5.7`, `1.5.5`, `1.5.4` и т. д.

```sh
cd /volume1/docker/meterzone

# 1. скачать свежий образ
sudo docker compose pull

# 2. пересоздать контейнер на новом образе (данные остаются)
sudo docker compose up -d

# 3. проверить, что поднялось и какая версия
sudo docker compose ps
curl -s http://127.0.0.1:8099/healthz
```

Если в системе нет `docker compose` (старый DSM), те же команды через дефис:
`sudo docker-compose pull` и `sudo docker-compose up -d`.

Через интерфейс: Container Manager → **Проект** → `meterzone` →
**Действие → Остановить**, затем **Действие → Собрать** — проект заново
скачает `latest`. Если такого пункта нет, обновляйтесь командами выше.

### Откат на прежнюю версию

```sh
cd /volume1/docker/meterzone
sudo sed -i 's|meterzone-dist:latest|meterzone-dist:1.5.5|' docker-compose.yml
sudo docker compose pull && sudo docker compose up -d
```

После проверки верните `latest` тем же `sed` в обратную сторону.
Откат образа не откатывает данные: если в новой версии что-то менялось
в базе, надёжнее взять копию из `data/backups/`.

### Если NAS не может скачать образ

Соберите контейнер из исходников на самом NAS — для этого есть готовый
архив `MeterZone-docker-dsm-1.5.7.zip` (в нём `Dockerfile` и исходники):

```sh
cd /volume1/docker
sudo unzip -o /volume1/public/MeterZone-docker-dsm-1.5.7.zip -d mz-new

# файлы проекта копируем поверх, не трогая data
sudo cp -rn mz-new/MeterZone/. mz/meterzone/

cd mz/meterzone
sudo docker compose up -d --build
```

В таком варианте `docker-compose.yml` должен собирать образ, а не тянуть его:
в нём вместо `image:` стоит

```yaml
services:
  meterzone:
    build:
      context: .
      dockerfile: Dockerfile
```

Первая сборка на NAS — несколько минут, дальше секунды.

## Перенос данных с роутера

База с Keenetic открывается как есть — переносится файлом, без конвертации.

1. На роутере остановите сервис, чтобы база была целой:
   ```sh
   /opt/etc/init.d/S99meterzone stop
   ```
2. Скачайте базу с роутера на компьютер:
   ```sh
   scp root@192.168.x.x:/opt/meterzone/data/meterzone.db ./meterzone.db
   ```
3. Положите файл в `/volume1/docker/meterzone/data/meterzone.db`
   (в File Station: папка `docker/meterzone/data`).
4. Запустите проект в Container Manager.

Если SSH к роутеру не хочется — сделайте это через интерфейс:
на роутере «Настройки» → «Создать копию» → скачать `.db` файл;
на NAS положите его в `data/meterzone.db` до первого запуска.

Проверить, что данные на месте, можно в «Настройках» приложения:
должны совпасть число объектов, приборов и показаний.

## Резервные копии

- Внутри NAS: `docker/meterzone/data/backups/` — их же можно скачать
  из интерфейса приложения на странице «Настройки».
- Дополнительно снаружи NAS: Hyper Backup — включите папку
  `docker/meterzone/data` в задание резервного копирования.
  Это защищает от поломки самого NAS, чего копии внутри не делают.

## Порт

Порт задан в `docker-compose.yml` в двух местах — `ports` и `PORT`,
и должен совпадать:

```yaml
    ports:
      - "8099:8099"
    environment:
      PORT: "8099"
```

Менять его из интерфейса приложения нельзя (там будет подсказка) —
иначе контейнер «уедет» на непроброшенный порт. После правки пересоздайте проект:

```sh
sudo docker compose up -d
```

Если 8099 занят, замените оба значения, например на `8090`.

## Переменные окружения

| Переменная | Значение в compose | Что задаёт |
|---|---|---|
| `PORT` | 8099 | порт HTTP внутри контейнера |
| `HOST` | 0.0.0.0 | адрес прослушивания |
| `APP_DB` | /data/meterzone.db | файл базы |
| `APP_BACKUP_DIR` | /data/backups | каталог копий |
| `APP_TZ` | Europe/Astrakhan | часовой пояс |
| `PORT_FIXED` | 1 | запрет смены порта из интерфейса |
| `ALLOW_PUBLIC` | — | `1` пускает не только локальные адреса |

## Доступ снаружи

По умолчанию приложение пускает только локальные и частные адреса.
Если нужен вход через внешний адрес или VPN с публичным IP,
раскомментируйте в `docker-compose.yml`:

```yaml
      ALLOW_PUBLIC: "1"
```

Через обратный прокси DSM (вход по HTTPS с сертификатом) запросы приходят
с внутреннего адреса, поэтому `ALLOW_PUBLIC` для этого не требуется.

## Диагностика

```sh
# запущенные контейнеры
sudo docker ps --filter name=meterzone

# журнал
sudo docker logs --tail 50 meterzone-meterzone-1

# здоровье и версия
curl -s http://127.0.0.1:8099/healthz

# какой образ и тег сейчас работают
sudo docker inspect --format '{{.Config.Image}}' meterzone-meterzone-1

# данные внутри тома
sudo ls -la /volume1/docker/meterzone/data
```

Если контейнер сразу останавливается — почти всегда права на папку `data`.
Проверьте, что она существует и доступна на запись; проще всего удалить её
и запустить проект заново, приложение создаст её само.

Если `docker compose pull` не может скачать образ (`ghcr.io`), проверьте
доступ NAS в интернет: Container Manager → **Реестр** → **Настройки** →
при необходимости зеркало реестра. Без интернета собирайте из архива
(см. «Если NAS не может скачать образ»).
