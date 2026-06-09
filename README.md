# EventHub

[![EventHub CI](https://github.com/AndrewTheMaster/NOSQL-course/actions/workflows/eventhub.yml/badge.svg)](https://github.com/AndrewTheMaster/NOSQL-course/actions/workflows/eventhub.yml)
![Version](https://img.shields.io/badge/version-1.0-blue)
![Go](https://img.shields.io/badge/Go-1.25.1-00ADD8?logo=go&logoColor=white)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

Backend-сервис платформы мероприятий: регистрация пользователей, каталог событий, реакции, отзывы и персональные рекомендации. Данные распределены между Redis, MongoDB, Cassandra и Neo4j.

## Содержание

- [Технологический стек](#технологический-стек)
- [Архитектура проекта](#архитектура-проекта)
- [Функциональные требования](#функциональные-требования)
- [API](#api)
- [Инструкция по запуску](#инструкция-по-запуску)
- [Конфигурация](#конфигурация)
- [Тестирование](#тестирование)
- [FAQ](#faq)

## Технологический стек

| Категория | Технология |
|-----------|------------|
| Язык | Go 1.25.1 |
| HTTP | стандартная библиотека `net/http` |
| Сборка | Docker multi-stage (`Dockerfile`), локально — `go build ./cmd/app` |
| Оркестрация | Docker Compose, команды — `Makefile` |
| Redis 7 | сессии, кэш реакций, отзывов и рекомендаций |
| MongoDB 7 | пользователи и мероприятия (шардированный кластер через `mongos`) |
| Apache Cassandra 4.1 | лайки, дизлайки, отзывы |
| Neo4j 5 | граф лайков для рекомендаций |

**Основные библиотеки:**

| Библиотека | Назначение |
|------------|------------|
| `github.com/redis/go-redis/v9` | клиент Redis |
| `go.mongodb.org/mongo-driver` | клиент MongoDB |
| `github.com/gocql/gocql` | клиент Cassandra |
| `github.com/neo4j/neo4j-go-driver/v5` | клиент Neo4j |
| `golang.org/x/crypto/bcrypt` | хеширование паролей |

## Архитектура проекта

### Структура каталогов

```
.
├── cmd/app/                 # точка входа и HTTP-обработчики
│   ├── main.go              # инициализация, маршруты
│   ├── session.go           # сессии в Redis
│   ├── auth.go              # login / logout
│   ├── users_handlers.go    # пользователи
│   ├── events.go            # мероприятия
│   ├── reactions.go         # лайки / дизлайки + кэш
│   ├── reviews.go           # отзывы + кэш
│   ├── recommendations.go   # рекомендации + кэш
│   ├── neo4j.go             # граф LIKED
│   ├── mongo.go             # модели и индексы MongoDB
│   └── cassandra.go         # подключение Cassandra
├── api/                     # Postman-коллекция
├── scripts/                 # init MongoDB, Cassandra
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── .env.local               # конфигурация окружения
```

### Схема взаимодействия компонентов

```mermaid
flowchart TB
    Client([HTTP Client]) --> App[Go App :8080]

    App --> Redis[(Redis)]
    App --> Mongo[(MongoDB mongos)]
    App --> Cass[(Cassandra)]
    App --> Neo4j[(Neo4j)]

    subgraph Redis
        R1[sid:* — сессии]
        R2[event:*:reactions — кэш лайков]
        R3[event:*:reviews — кэш отзывов]
        R4[user:*:recomms — кэш рекомендаций]
    end

    subgraph MongoDB
        M1[(users)]
        M2[(events)]
    end

    subgraph Cassandra
        C1[event_reactions]
        C2[event_reviews]
        C3[event_reviews_timeline]
    end

    subgraph Neo4j
        N1[User]-[:LIKED]->N2[Event]
    end
```

### Основные сущности

**User** (MongoDB, коллекция `users`):

| Поле | Тип | Описание |
|------|-----|----------|
| `_id` | ObjectId | идентификатор |
| `full_name` | string | имя |
| `username` | string | логин (уникальный) |
| `password_hash` | string | bcrypt-хеш пароля |

**Event** (MongoDB, коллекция `events`):

| Поле | Тип | Описание |
|------|-----|----------|
| `_id` | ObjectId | идентификатор |
| `title` | string | название |
| `description` | string | описание |
| `category` | string | meetup / concert / exhibition / party / other |
| `price` | uint64 | цена |
| `location.city`, `location.address` | string | место проведения |
| `created_at`, `started_at`, `finished_at` | string (RFC3339) | даты |
| `created_by` | string | id организатора |

**Связи:**

- User **создаёт** Event (`created_by`)
- User **лайкает** Event → Cassandra `event_reactions` + Neo4j `(User)-[:LIKED]->(Event)`
- User **оставляет** Review на Event → Cassandra `event_reviews`
- Рекомендации строятся по графу Neo4j, полные данные Event подгружаются из MongoDB

Логическая схема хранилищ (DBML): [docs/db/schema.dbml](docs/db/schema.dbml) — можно открыть на [dbdiagram.io](https://dbdiagram.io).

## Функциональные требования

### Use Cases

| # | Сценарий | Эндпоинты |
|---|----------|-----------|
| 1 | Гость проверяет доступность сервиса | `GET /health` |
| 2 | Гость получает анонимную сессию | `POST /session` |
| 3 | Пользователь регистрируется и входит | `POST /users`, `POST /auth/login` |
| 4 | Организатор создаёт и редактирует мероприятие | `POST /events`, `PATCH /events/{id}` |
| 5 | Пользователь ищет мероприятия по фильтрам | `GET /events`, `GET /users/{id}/events` |
| 6 | Пользователь ставит лайк или дизлайк | `POST /events/{id}/like`, `POST /events/{id}/dislike` |
| 7 | Пользователь читает и пишет отзывы | `GET/POST/PATCH /events/{id}/reviews` |
| 8 | Пользователь получает персональные рекомендации | `GET /recommendations` |

### Реализованный функционал по этапам разработки

**Сессии (Redis):** cookie `X-Session-Id`, TTL из `APP_USER_SESSION_TTL`; `GET /health` не продлевает TTL.

**Пользователи и события (MongoDB):** CRUD пользователей, CRUD событий, фильтрация, авторизация по сессии.

**Шардирование MongoDB:** config server, два шарда, `mongos`; `PATCH /events/{id}` — только для организатора.

**Реакции (Cassandra + Redis):** агрегация по названию события, кэш `event:{md5(title)}:reactions`, `?include=reactions`.

**Отзывы (Cassandra + Redis):** один отзыв на пользователя и событие, кэш `event:{md5(title)}:reviews`, `?include=reviews`.

**Рекомендации (Neo4j + Redis):** collaborative filtering по лайкам; дизлайк не удаляет `LIKED`; кэш `user:{user_id}:recomms`.

## API

Postman-коллекция: **[api/eventhub.postman_collection.json](api/eventhub.postman_collection.json)**  
Подробнее об импорте и порядке вызовов: **[api/README.md](api/README.md)**.

Базовый URL: `http://localhost:8080` (переменная `baseUrl` в коллекции).

Авторизация — cookie `X-Session-Id`, получаемая через `POST /session` и `POST /auth/login`.

### Таблица эндпоинтов

| Метод | Путь | Auth | Описание |
|-------|------|------|----------|
| GET | `/health` | — | Healthcheck |
| POST | `/session` | — | Создать / продлить сессию |
| POST | `/users` | — | Регистрация |
| GET | `/users` | — | Список пользователей |
| GET | `/users/{id}` | — | Профиль |
| GET | `/users/{id}/events` | — | События организатора |
| POST | `/auth/login` | — | Вход |
| POST | `/auth/logout` | ✓ | Выход |
| GET | `/events` | — | Список событий |
| POST | `/events` | ✓ | Создать событие |
| GET | `/events/{id}` | — | Событие по id |
| PATCH | `/events/{id}` | ✓ | Изменить category / price / city |
| POST | `/events/{id}/like` | ✓ | Лайк |
| POST | `/events/{id}/dislike` | ✓ | Дизлайк |
| GET | `/events/{id}/reviews` | — | Список отзывов |
| POST | `/events/{id}/reviews` | ✓ | Создать отзыв |
| PATCH | `/events/{id}/reviews/{rid}` | ✓ | Изменить свой отзыв |
| GET | `/recommendations` | ✓ | Рекомендации |

### Примеры запросов и ответов

#### Healthcheck

```http
GET /health HTTP/1.1
Host: localhost:8080
```

```http
HTTP/1.1 200 OK
Content-Type: application/json

{"status":"ok"}
```

#### Регистрация

```http
POST /users HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{"full_name":"Иван Иванов","username":"ivan","password":"secret123"}
```

```http
HTTP/1.1 201 Created
Set-Cookie: X-Session-Id=...; HttpOnly; Path=/; Max-Age=60
```

#### Создание мероприятия

```http
POST /events HTTP/1.1
Host: localhost:8080
Cookie: X-Session-Id=...
Content-Type: application/json

{
  "title": "Концерт в парке",
  "address": "г. Москва, парк Горького",
  "description": "Летний концерт",
  "started_at": "2026-06-01T18:00:00+03:00",
  "finished_at": "2026-06-01T21:00:00+03:00"
}
```

```http
HTTP/1.1 201 Created
Content-Type: application/json

{"id":"65e9c0b1a2b3c4d5e6f7a8b9"}
```

#### Список мероприятий с реакциями и отзывами

```http
GET /events?limit=10&include=reactions,reviews HTTP/1.1
Host: localhost:8080
```

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
      "reactions": {"likes": 12, "dislikes": 1},
      "reviews": {"count": 5, "rating": 4.2}
    }
  ],
  "count": 1
}
```

#### Лайк

```http
POST /events/65e9c0b1a2b3c4d5e6f7a8b9/like HTTP/1.1
Host: localhost:8080
Cookie: X-Session-Id=...
```

```http
HTTP/1.1 204 No Content
```

#### Создание отзыва

```http
POST /events/65e9c0b1a2b3c4d5e6f7a8b9/reviews HTTP/1.1
Host: localhost:8080
Cookie: X-Session-Id=...
Content-Type: application/json

{"comment": "Отличное мероприятие!", "rating": 5}
```

```http
HTTP/1.1 201 Created
Content-Type: application/json

{"id": "069b9830-4b5f-487a-ae89-424619ca2a35"}
```

#### Рекомендации

```http
GET /recommendations HTTP/1.1
Host: localhost:8080
Cookie: X-Session-Id=...
```

```json
{
  "events": [
    {
      "id": "12e9c0b1a2b3c3d5e6f7a8b7",
      "title": "Выставка российского зодчества",
      "category": "exhibition",
      "price": 0,
      "description": "Описание выставки",
      "location": {"city": "Москва", "address": "ул. Примерная, 1"},
      "created_at": "2026-03-14T14:59:32+03:00",
      "created_by": "65e9c0b1a2b3c4d5e6f7a8b9",
      "started_at": "2026-04-01T12:00:00+03:00",
      "finished_at": "2026-04-01T23:00:00+03:00"
    }
  ]
}
```

Без авторизации:

```http
GET /recommendations HTTP/1.1
Host: localhost:8080
```

```http
HTTP/1.1 401 Unauthorized
```

## Инструкция по запуску

### Требования

- Docker и Docker Compose
- Make
- Git

### Пошаговый запуск

1. Клонируйте репозиторий:

   ```bash
   git clone https://github.com/AndrewTheMaster/NOSQL-course.git
   cd NOSQL-course
   ```

2. Убедитесь, что файл [`.env.local`](.env.local) на месте (значения по умолчанию подходят для локального Docker).

3. Запустите все сервисы:

   ```bash
   make run
   ```

   Первый запуск занимает несколько минут: инициализируются MongoDB (replica sets, шарды), Cassandra, Neo4j.

4. Проверьте статус контейнеров:

   ```bash
   make services
   ```

5. Проверьте API:

   ```bash
   make health
   ```

6. (Опционально) Импортируйте [Postman-коллекцию](api/eventhub.postman_collection.json) и выполните запросы.

### Остановка

```bash
make stop          # остановить контейнеры
make clean         # остановить и удалить volumes
make restart       # перезапуск
make logs          # логи всех сервисов
make logs-app      # логи приложения
```

Полный список команд: `make help`.

### Локальная сборка без Docker (опционально)

```bash
go build -o bin/app ./cmd/app
```

Для работы нужны доступные инстансы Redis, MongoDB, Cassandra и Neo4j с параметрами из `.env.local`.

## Конфигурация

Вся конфигурация — в файле [`.env.local`](.env.local). Docker Compose и приложение читают его через `env_file`.

| Переменная | Описание | Значение по умолчанию |
|------------|----------|----------------------|
| `APP_HOST` | Хост для внешних проверок (CI) | `localhost` |
| `APP_PORT` | Порт HTTP-сервера | `8080` |
| `APP_USER_SESSION_TTL` | TTL сессии в Redis (сек) | `60` |
| `APP_LIKE_TTL` | TTL кэша реакций (сек) | `60` |
| `APP_EVENT_REVIEWS_TTL` | TTL кэша отзывов (сек) | `120` |
| `APP_RECOMMENDATIONS_TTL` | TTL кэша рекомендаций (сек) | `60` |
| `REDIS_HOST` | Хост Redis | `redis` |
| `REDIS_PORT` | Порт Redis | `6379` |
| `REDIS_PASSWORD` | Пароль Redis | *(пусто)* |
| `REDIS_DB` | Номер БД Redis | `0` |
| `MONGODB_HOST` | Хост MongoDB (`mongos`) | `mongodb` |
| `MONGODB_PORT` | Порт MongoDB | `27017` |
| `MONGODB_DATABASE` | Имя базы | `eventhub` |
| `MONGODB_USER` | Пользователь MongoDB | *(пусто)* |
| `MONGODB_PASSWORD` | Пароль MongoDB | *(пусто)* |
| `CASSANDRA_HOSTS` | Хосты Cassandra (через запятую) | `cassandra` |
| `CASSANDRA_PORT` | Порт Cassandra | `9042` |
| `CASSANDRA_KEYSPACE` | Keyspace | `testkeyspace` |
| `CASSANDRA_USERNAME` | Пользователь Cassandra | *(пусто)* |
| `CASSANDRA_PASSWORD` | Пароль Cassandra | *(пусто)* |
| `CASSANDRA_CONSISTENCY` | Уровень консистентности | `ONE` |
| `NEO4J_URL` | Bolt URI Neo4j | `bolt://neo4j:7687` |
| `NEO4J_USER` | Пользователь Neo4j | `neo4j` |
| `NEO4J_USERNAME` | Альias для CI-автогрейдера | `neo4j` |
| `NEO4J_PASSWORD` | Пароль Neo4j | `password` |
| `NEO4J_BOLT_PORT` | Проброс Bolt-порта на хост | `7687` |

Внутри Docker-сети сервисы доступны по именам контейнеров. С хоста — через проброшенные порты (`APP_PORT`, `MONGODB_PORT`, и т.д.).

## Тестирование

### Автоматические проверки (CI)

При push / pull request GitHub Actions запускает автогрейдер курса. Номер проверяемой лабораторной задаётся в [`.labrc`](.labrc) (`LAB=7`).

```bash
# локально — после make run, из репозитория ndbx:
# go run autograder/cmd/lab7/main.go
# с переменными окружения из .env.local и хостами localhost
```

### Ручное тестирование API

1. `make run`
2. Импорт [api/eventhub.postman_collection.json](api/eventhub.postman_collection.json) в Postman / Insomnia
3. Выполнение запросов по порядку из [api/README.md](api/README.md)

### Юнит- и интеграционные тесты

В репозитории нет отдельного пакета `*_test.go`. Основная проверка корректности — автогрейдер GitHub Actions и ручные запросы через Postman.

Локальная проверка сборки:

```bash
go build -o /dev/null ./cmd/app
```

