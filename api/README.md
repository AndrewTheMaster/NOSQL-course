# API-коллекция EventHub

## Postman

1. Импортируйте [eventhub.postman_collection.json](eventhub.postman_collection.json) в Postman.
2. Убедитесь, что проект запущен: `make run` из корня репозитория.
3. Переменная коллекции `baseUrl` по умолчанию: `http://localhost:8080`.
4. Включите сохранение cookies — для сессии `X-Session-Id` и авторизации.

## Рекомендуемый порядок запросов

1. `POST /session`
2. `POST /users` или `POST /auth/login`
3. `POST /events` (сохранится `eventId` в переменные коллекции)
4. `POST /events/{id}/like` — для графа рекомендаций
5. `GET /recommendations`

## Insomnia

Импорт JSON коллекции Postman v2.1 поддерживается в Insomnia (Import → From File).
