# Карта заправок

Сервис показывает заправки на карте OpenStreetMap. Пользователь может сообщить, какое топливо есть на заправке: АИ-92, АИ-95 или ДТ. Рядом с каждой заправкой отображается суммарный счётчик голосов «есть» / «нет». Внутри popup счётчики разбиты по типу топлива. Каждый голос хранится 3 часа, после чего автоматически удаляется.

## Стек

- Backend: Go (stdlib `net/http`)
- Frontend: Vue 3 + Vite + Leaflet
- Карта: OpenStreetMap (тайлы) + Overpass API (заправки)

## Структура

```
zapravka/
├── backend/
│   ├── main.go
│   └── go.mod
├── frontend/
│   ├── .env              # переменные окружения (порт, URL backend)
│   ├── .env.example      # пример переменных
│   ├── index.html
│   ├── package.json
│   ├── vite.config.js
│   └── src/
│       ├── main.js
│       ├── App.vue
│       └── components/
│           └── MapView.vue
├── install.sh            # установка с нуля
├── start.sh              # запуск
├── stop.sh               # остановка
└── README.md
```

## Быстрая установка на чистый сервер

```bash
./install.sh  # проверяет и устанавливает Go, Node.js, npm, git, зависимости проекта
./start.sh    # запускает backend и frontend
```

Поддерживаются:
- Ubuntu/Debian (apt)
- RHEL/CentOS (yum)
- macOS (Homebrew)

## Настройка портов

Файл `frontend/.env`:

```env
VITE_PORT=80
VITE_API_URL=http://localhost:8081
```

- `VITE_PORT` — порт frontend (по умолчанию `80`).
- `VITE_API_URL` — URL backend для проксирования API.

Backend настраивается переменными окружения:

```bash
HOST=0.0.0.0 PORT=8081 ./backend/zapravka
```

По умолчанию backend слушает `0.0.0.0:8081`, frontend — `0.0.0.0:80`.

> **Важно:** порты ниже 1024 (например, 80) требуют root-прав на Linux/macOS. `install.sh` и `start.sh` на Linux должны запускаться от root.

## Запуск

```bash
./start.sh   # запускает backend и frontend в фоне, проверяет зависимости
./stop.sh    # останавливает их
```

Или через systemd (после `./install.sh`):

```bash
sudo systemctl enable --now zapravka
```

## API

### Получить заправки

```http
GET /api/stations?lat={lat}&lon={lon}&radius={meters}
```

Пример:

```bash
curl "http://localhost:8081/api/stations?lat=55.7558&lon=37.6173&radius=5000"
```

Ответ:

```json
{
  "stations": [
    {
      "id": "272607919",
      "lat": 55.7508202,
      "lon": 37.6583094,
      "name": "Татнефть",
      "counts": {
        "92": { "yes": 0, "no": 1 },
        "95": { "yes": 1, "no": 0 },
        "diesel": { "yes": 1, "no": 0 }
      }
    }
  ]
}
```

### Проголосовать

```http
POST /api/vote
Content-Type: application/json

{
  "station_id": "272607919",
  "fuel_type": "95",   // "92", "95" или "diesel"
  "type": "yes"        // "yes" или "no"
}
```

## Особенности

- Голоса хранятся в памяти backend. Один экземпляр = одна нода.
- Каждый голос действует ровно 3 часа, после чего удаляется фоновой очисткой.
- Приложение запрашивает геолокацию пользователя и центрирует карту по ней.
- При перемещении карты заправки подгружаются заново для новой области.
