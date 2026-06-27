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
│   ├── index.html
│   ├── package.json
│   ├── vite.config.js
│   └── src/
│       ├── main.js
│       ├── App.vue
│       └── components/
│           └── MapView.vue
├── start.sh
├── stop.sh
└── README.md
```

## Запуск

В корне проекта есть удобные скрипты:

```bash
./start.sh   # запускает backend и frontend в фоне
./stop.sh    # останавливает их
```

Или вручную:

### Backend

```bash
cd backend
go run .
# http://localhost:8081
```

### Frontend

```bash
cd frontend
npm install
npm run dev
# http://localhost:5173
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
