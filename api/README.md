# EventHub — API

Postman Collection v2.1 для ручного тестирования HTTP API.

## Файлы

| Файл | Описание |
|------|----------|
| [eventhub.postman_collection.json](eventhub.postman_collection.json) | все эндпоинты, сгруппированные по лабам |

## Импорт

### Postman

1. **Import** → выберите `eventhub.postman_collection.json`
2. Переменная коллекции `baseUrl` = `http://localhost:8080`
3. **Settings → Cookies** — включите автоматическое сохранение cookies

### Insomnia

**Import → From File** — формат Postman v2.1 поддерживается.

## Порядок вызовов

1. `POST /session` — получить cookie `X-Session-Id`
2. `POST /users` или `POST /auth/login` — авторизация
3. `POST /events` — создать мероприятие (`eventId` сохранится в переменные)
4. `POST /events/{id}/like` — для графа рекомендаций
5. `GET /recommendations` — персональные рекомендации

## Примеры запросов и ответов

### POST /session

**Запрос:**

```http
POST /session HTTP/1.1
Host: localhost:8080
```

**Ответ (первый визит):**

```http
HTTP/1.1 201 Created
Set-Cookie: X-Session-Id=3f8a2c1d9e4b7f0a5c6d2e8b1a3f9c7d; HttpOnly; Path=/; Max-Age=60
```

**Ответ (повторный визит):**

```http
HTTP/1.1 200 OK
Set-Cookie: X-Session-Id=3f8a2c1d9e4b7f0a5c6d2e8b1a3f9c7d; HttpOnly; Path=/; Max-Age=60
```

### POST /users

**Запрос:**

```json
{
  "full_name": "Иван Иванов",
  "username": "ivan",
  "password": "secret123"
}
```

**Ответ:**

```http
HTTP/1.1 201 Created
```

### POST /auth/login

**Запрос:**

```json
{
  "username": "ivan",
  "password": "secret123"
}
```

**Ответ:**

```http
HTTP/1.1 204 No Content
Set-Cookie: X-Session-Id=...; HttpOnly; Path=/; Max-Age=60
```

**Ошибка:**

```http
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{"message": "invalid credentials"}
```

### POST /events

**Запрос:**

```json
{
  "title": "Концерт в парке",
  "address": "г. Москва, парк Горького",
  "description": "Летний концерт",
  "started_at": "2026-06-01T18:00:00+03:00",
  "finished_at": "2026-06-01T21:00:00+03:00"
}
```

**Ответ:**

```http
HTTP/1.1 201 Created
Content-Type: application/json

{"id": "65e9c0b1a2b3c4d5e6f7a8b9"}
```

### GET /events?include=reactions,reviews

**Ответ:**

```json
{
  "events": [
    {
      "id": "65e9c0b1a2b3c4d5e6f7a8b9",
      "title": "Концерт в парке",
      "category": "other",
      "price": 0,
      "description": "Летний концерт",
      "location": {"address": "г. Москва, парк Горького"},
      "created_at": "2026-03-14T14:59:32+03:00",
      "created_by": "53e9c0c1a2a3c3d7e6c9c8a1",
      "started_at": "2026-06-01T18:00:00+03:00",
      "finished_at": "2026-06-01T21:00:00+03:00",
      "reactions": {"likes": 3, "dislikes": 0},
      "reviews": {"count": 2, "rating": 4.5}
    }
  ],
  "count": 1
}
```

### POST /events/{id}/like

**Ответ:**

```http
HTTP/1.1 204 No Content
```

### POST /events/{id}/reviews

**Запрос:**

```json
{
  "comment": "Отличное мероприятие!",
  "rating": 5
}
```

**Ответ:**

```http
HTTP/1.1 201 Created
Content-Type: application/json

{"id": "069b9830-4b5f-487a-ae89-424619ca2a35"}
```

**Конфликт (повторный отзыв):**

```http
HTTP/1.1 409 Conflict
Content-Type: application/json

{"message": "Already exists"}
```

### GET /recommendations

**Ответ (есть рекомендации):**

```json
{
  "events": [
    {
      "id": "12e9c0b1a2b3c3d5e6f7a8b7",
      "title": "Выставка",
      "category": "exhibition",
      "price": 0,
      "description": "...",
      "location": {"city": "Москва", "address": "..."},
      "created_at": "2026-03-14T14:59:32+03:00",
      "created_by": "65e9c0b1a2b3c4d5e6f7a8b9",
      "started_at": "2026-04-01T12:00:00+03:00",
      "finished_at": "2026-04-01T23:00:00+03:00"
    }
  ]
}
```

**Ответ (пусто):**

```json
{"events": []}
```

**Без авторизации:**

```http
HTTP/1.1 401 Unauthorized
```
