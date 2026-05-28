Backend-сервис платформы мероприятий (EventHub) для практического изучения NoSQL: Redis, MongoDB, Apache Cassandra и Neo4j. Функциональность наращивается по лабораторным работам 1–7.

## Содержание

- [Быстрый старт](#быстрый-старт)
- [Архитектура](#архитектура)
- [Лабораторные работы](#лабораторные-работы)
- [API](#api)
- [Конфигурация](#конфигурация)
- [Makefile](#makefile)
- [Структура проекта](#структура-проекта)

## Быстрый старт

### Требования

- Docker и Docker Compose
- Make
- (опционально) [Postman](https://www.postman.com/) или Insomnia для ручного тестирования API

### Запуск

```bash
make run
```

Сервис будет доступен на `http://localhost:8080` (порт задаётся в `.env.local` → `APP_PORT`).

Проверка:

```bash
make health
# {"status":"ok"}
```

Остановка:

```bash
make stop
```

Полная очистка с томами:

```bash
make clean
```

Первый запуск может занять несколько минут: поднимаются MongoDB (шардированный кластер), Cassandra, Redis, Neo4j и инициализируются схемы.

## Архитектура

```
                    ┌─────────────┐
                    │   Client    │
                    └──────┬──────┘
                           │ HTTP
                    ┌──────▼──────┐
                    │  Go App     │
                    │  (cmd/app)  │
                    └──┬──┬──┬──┬┘
         ┌─────────────┘  │  │  └─────────────┐
         ▼                ▼  ▼                ▼
   ┌──────────┐    ┌──────────┐    ┌──────────────┐
   │  Redis   │    │ MongoDB  │    │  Cassandra   │
   │ сессии,  │    │ users,   │    │ лайки/       │
   │ кэш      │    │ events   │    │ дизлайки,    │
   │          │    │          │    │ отзывы       │
   └──────────┘    └──────────┘    └──────────────┘
                           │
                    ┌──────▼──────┐
                    │   Neo4j     │
                    │ граф LIKED  │
                    │ (рекоменда- │
                    │  ции)       │
                    └─────────────┘
```

| Хранилище | Назначение |
|-----------|------------|
| **Redis** | Сессии (`sid:*`), кэш реакций и отзывов по названию события, кэш рекомендаций пользователя |
| **MongoDB** | Пользователи, мероприятия (основные данные) |
| **Cassandra** | Лайки/дизлайки (`event_reactions`), отзывы (`event_reviews`) |
| **Neo4j** | Граф пользователей и событий, связь `LIKED` для collaborative filtering |

Конфигурация — только через [`.env.local`](.env.local) (см. [CONTRIBUTING.md](CONTRIBUTING.md)).

## Лабораторные работы

Ниже — что реализовано в приложении на каждом этапе курса.

### Lab 1 — Healthcheck

- `GET /health` — проверка доступности, ответ `{"status":"ok"}`

### Lab 2 — Сессии (Redis)

- `POST /session` — создание (201) или продление (200) анонимной сессии
- Cookie `X-Session-Id`, TTL из `APP_USER_SESSION_TTL`
- `GET /health` возвращает существующую cookie без продления TTL

### Lab 3 — Пользователи и события (MongoDB)

- Регистрация: `POST /users`
- Список / профиль: `GET /users`, `GET /users/{id}`
- События пользователя: `GET /users/{id}/events`
- Авторизация: `POST /auth/login`, `POST /auth/logout`
- События: `POST /events`, `GET /events`, `GET /events/{id}`, `PATCH /events/{id}`
- Фильтрация списка событий (title, category, price, city, даты и др.)

### Lab 4 — MongoDB: шардирование и доработка событий

- Шардированный кластер MongoDB в Docker (config server, шарды, `mongos`)
- `PATCH /events/{id}` — изменение `category`, `price`, `city` (только организатор)
- Расширенные фильтры в `GET /events` и `GET /users/{id}/events`

### Lab 5 — Лайки и дизлайки (Cassandra + Redis)

- `POST /events/{id}/like`, `POST /events/{id}/dislike`
- Хранение в Cassandra (`event_reactions`), один пользователь — одна реакция на событие
- Агрегация по **названию** мероприятия (несколько событий с одним title суммируются)
- Кэш в Redis: `event:{md5(title)}:reactions` (поля `likes`, `dislikes`), TTL `APP_LIKE_TTL`
- Сводка в ответах: `?include=reactions` (можно комбинировать: `?include=reactions,reviews`)

### Lab 6 — Отзывы (Cassandra + Redis)

- `POST /events/{id}/reviews` — создать отзыв (`comment` до 300 символов, `rating` 1–5); один пользователь — один отзыв на событие (повтор → `409 Already exists`)
- `GET /events/{id}/reviews` — список с `limit` / `offset` (пагинация в приложении поверх выборки из `event_reviews_timeline`, сортировка по `created_at` DESC)
- `PATCH /events/{id}/reviews/{review_id}` — частичное обновление своего отзыва, обновляется `updated_at`
- Таблицы Cassandra: `event_reviews` (уникальность по `event_id` + `created_by`), `event_reviews_timeline` (лента по событию)
- Кэш сводки по названию: `event:{md5(title)}:reviews` (`count`, средний `rating` с округлением до 0.1), TTL `APP_EVENT_REVIEWS_TTL`; обновление после создания и изменения отзыва
- В карточке и списке событий: `?include=reviews` → объект `reviews: {count, rating}` (в т.ч. нули, если отзывов нет)

### Lab 7 — Рекомендации (Neo4j + Redis)

- `GET /recommendations` — только для авторизованного пользователя (свои рекомендации)
- Алгоритм: пользователи с общими лайками → их другие лайки, исключая уже лайкнутые
- Дизлайк **не удаляет** связь `LIKED` в Neo4j
- Дедупликация по `title` (ближайший `started_at`), сортировка по популярности
- Кэш Cache-Aside: Redis hash `user:{user_id}:recomms`, поле `events`, TTL `APP_RECOMMENDATIONS_TTL`

Граф Neo4j обновляется при создании пользователя, события и **лайка**.

## API

Полная коллекция запросов: **[api/eventhub.postman_collection.json](api/eventhub.postman_collection.json)**.

Импорт в Postman: **Import** → выберите файл. Переменная коллекции `baseUrl` по умолчанию `http://localhost:8080`. Для сессий включите сохранение cookies в Postman.

### Краткая таблица эндпоинтов

| Метод | Путь | Авторизация | Описание |
|-------|------|-------------|----------|
| GET | `/health` | — | Healthcheck |
| POST | `/session` | — | Создать/продлить сессию |
| POST | `/users` | — | Регистрация |
| GET | `/users` | — | Список пользователей |
| GET | `/users/{id}` | — | Профиль |
| GET | `/users/{id}/events` | — | События пользователя |
| POST | `/auth/login` | — | Вход (привязка user_id к сессии) |
| POST | `/auth/logout` | да | Выход |
| GET | `/events` | — | Список событий |
| POST | `/events` | да | Создать событие |
| GET | `/events/{id}` | — | Событие по id |
| PATCH | `/events/{id}` | да (организатор) | Обновить category/price/city |
| POST | `/events/{id}/like` | да | Лайк |
| POST | `/events/{id}/dislike` | да | Дизлайк |
| GET | `/events/{id}/reviews` | — | Отзывы |
| POST | `/events/{id}/reviews` | да | Создать отзыв |
| PATCH | `/events/{id}/reviews/{rid}` | да (автор) | Изменить отзыв |
| GET | `/recommendations` | да | Рекомендации мероприятий |

### Пример сценария (curl)

```bash
# Сессия
curl -c cookies.txt -b cookies.txt -X POST http://localhost:8080/session

# Регистрация
curl -c cookies.txt -b cookies.txt -X POST http://localhost:8080/users \
  -H 'Content-Type: application/json' \
  -d '{"full_name":"Иван","username":"ivan","password":"secret123"}'

# Создание события
curl -c cookies.txt -b cookies.txt -X POST http://localhost:8080/events \
  -H 'Content-Type: application/json' \
  -d '{"title":"Концерт","address":"Москва","started_at":"2026-06-01T18:00:00+03:00","finished_at":"2026-06-01T21:00:00+03:00"}'

# Рекомендации (после лайков в системе)
curl -c cookies.txt -b cookies.txt http://localhost:8080/recommendations
```

## Конфигурация

Файл [`.env.local`](.env.local):

| Переменная | Описание |
|------------|----------|
| `APP_PORT` | Порт HTTP-сервера |
| `APP_USER_SESSION_TTL` | TTL сессии в Redis (сек) |
| `APP_LIKE_TTL` | TTL кэша реакций |
| `APP_EVENT_REVIEWS_TTL` | TTL кэша отзывов |
| `APP_RECOMMENDATIONS_TTL` | TTL кэша рекомендаций |
| `REDIS_HOST`, `REDIS_PORT`, … | Redis |
| `MONGODB_*` | MongoDB (mongos) |
| `CASSANDRA_*` | Cassandra |
| `NEO4J_URL`, `NEO4J_USER`, `NEO4J_PASSWORD` | Neo4j |
| `NEO4J_BOLT_PORT` | Проброс Bolt-порта на хост |

Внутри Docker приложение подключается к сервисам по именам (`redis`, `mongodb`, `cassandra`, `neo4j`). Для локального автогрейдера CI порты пробрасываются на `localhost`.

## Makefile

```bash
make help      # список команд
make run       # запуск в фоне (docker compose up -d --build)
make rund      # запуск с логами в консоли
make stop      # остановка
make clean     # остановка + удаление volumes
make services  # статус контейнеров
make logs      # логи всех сервисов
make logs-app  # логи только приложения
make health    # curl GET /health
make restart   # stop + run
```

## Структура проекта

```
.
├── cmd/app/           # HTTP-сервер и обработчики
├── api/               # Postman-коллекция
├── scripts/           # инициализация MongoDB, Cassandra
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── .env.local         # конфигурация (не коммитить секреты в публичный репо при необходимости)
└── .labrc             # номер лабы для CI (LAB=7)
```

