<div align="center">

# 🎮 MinecraftSkins

### Магазин Minecraft-скинов на Go с PostgreSQL и динамическими ценами в BTC

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![API](https://img.shields.io/badge/API-REST-2496ED)](api/openapi.yaml)
[![CI](https://github.com/fairwix/MinecraftSkins-Go/actions/workflows/ci.yml/badge.svg)](https://github.com/fairwix/MinecraftSkins-Go/actions/workflows/ci.yml)

**Go-перенос backend части проекта [MinecraftSkins](https://github.com/fairwix/MinecraftSkins).**
Исходный фронтенд сохранён и работает с новым API через Nginx.

</div>

## О проекте

MinecraftSkins — учебный backend каталога скинов. При просмотре каталога приложение получает актуальный курс BTC/USD и рассчитывает цену. При покупке оно сохраняет чек с курсом и итоговой стоимостью, поэтому история не меняется вслед за рынком.

В репозитории показаны практические задачи Go backend-разработки: REST API на `net/http`, разделение обработчиков и бизнес-логики, параметризованный SQL, транзакции PostgreSQL, внешняя интеграция с кэшем и fallback, идемпотентные запросы, Docker и автоматические проверки.

> Это демонстрационный проект. Заголовок `X-User-Id` имитирует пользователя и не является системой аутентификации.

## Возможности

- Каталог с поиском, фильтром доступности и пагинацией.
- Динамическая цена с округлением до центов.
- Курс BTC/USD из CoinGecko: кэш на 60 секунд и fallback на последнее успешное значение до 10 минут.
- Покупки с сохранением цены и курса, историей и `Idempotency-Key`.
- Уникальность ключа идемпотентности в пределах покупателя.
- Soft delete скинов; историю покупок сохраняет внешний ключ с `ON DELETE RESTRICT`.
- Health check, OpenAPI-спецификация и локальный интерфейс.

В исходном проекте нет загрузки PNG, обработки изображений, регистрации или настоящей авторизации. Эти функции не добавлялись.

## Стек

| Область | Технологии |
| --- | --- |
| Язык и HTTP | Go 1.27, `net/http`, `ServeMux` |
| Хранилище | PostgreSQL 16, `pgx/v5`, SQL |
| Миграции | [Goose](https://github.com/pressly/goose) |
| Деньги | `shopspring/decimal` |
| Конфигурация и логи | Переменные окружения, `log/slog` |
| Внешний курс | CoinGecko API, `net/http`, потокобезопасный кэш |
| Инфраструктура | Docker, Compose, Nginx |
| Проверки | `testing`, `httptest`, PostgreSQL integration tests, GitHub Actions |

## Архитектура

```mermaid
flowchart LR
    Browser[Браузер] --> Nginx[Nginx: статика и reverse proxy]
    Nginx --> API[HTTP API: net/http]
    API --> Service[Сервисы приложения]
    Service --> Repo[PostgreSQL-хранилище: pgx]
    Repo --> DB[(PostgreSQL)]
    Service --> Rate[Кэш курса BTC/USD]
    Rate --> CoinGecko[CoinGecko API]
```

Поток запроса проходит через HTTP-обработчик, сервис приложения и интерфейс репозитория. Обработчик отвечает за HTTP и JSON, сервис — за правила покупки и цены, репозиторий — за SQL. Во время покупки PostgreSQL блокирует строку скина, проверяет доступность и записывает чек в одной транзакции.

```text
cmd/api/                 запуск, подключение зависимостей и graceful shutdown
internal/config/         env-конфигурация
internal/domain/         модели, ошибки и формула цены
internal/service/        каталог и сценарий покупки
internal/postgres/       параметризованные запросы и транзакции
internal/ratestore/      CoinGecko, кэш, fallback и объединение запросов
internal/httpapi/        маршруты, валидация HTTP и ответы Problem Details
migrations/              SQL-миграции Goose
api/openapi.yaml         описание REST API
frontend/                исходный HTML/CSS/JavaScript интерфейс
```

### Расчёт цены

```text
Итоговая цена = базовая цена × (1 + курс BTC/USD ÷ 50 000)
```

Для расчётов используется десятичная арифметика и банковское округление до двух знаков — так избегаем ошибок двоичного `float64` и сохраняем правило исходного .NET приложения.

## Быстрый запуск

Нужны Docker и Docker Compose.

```bash
cp .env.example .env
docker compose up --build
```

Compose поднимет PostgreSQL, применит миграции Goose, запустит API и раздачу фронтенда через Nginx.

| Адрес | Назначение |
| --- | --- |
| <http://localhost:8080> | Веб-интерфейс |
| <http://localhost:8080/api/skins> | Каталог API |
| <http://localhost:8080/api/rates/btc-usd> | Курс BTC/USD |
| <http://localhost:8080/health> | Проверка API и зависимостей (`503`, если БД или курс недоступны) |
| `api/openapi.yaml` | OpenAPI 3.1 |

Остановить сервисы, сохранив данные:

```bash
docker compose down
```

Удалить контейнеры вместе с локальной базой:

```bash
docker compose down -v
```

## API

Полная схема запросов, параметров и ответов находится в [OpenAPI-файле](api/openapi.yaml).

| Метод | Маршрут | Назначение |
| --- | --- | --- |
| `GET` | `/api/skins` | Каталог: `availableOnly`, `search`, `skip`, `take`; общее число — в `X-Total-Count` |
| `GET` | `/api/skins/{id}` | Скин с актуальной ценой |
| `POST` | `/api/skins` | Создать скин |
| `PUT` | `/api/skins/{id}` | Обновить поля скина |
| `DELETE` | `/api/skins/{id}` | Soft delete |
| `GET` | `/api/rates/btc-usd` | Курс с источником и возрастом значения |
| `POST` | `/api/purchases` | Купить скин; заголовки `X-User-Id`, `Idempotency-Key` |
| `GET` | `/api/purchases` | История: `mineOnly`, `skinId`, `from`, `to`, `skip`, `take` |
| `GET` | `/api/purchases/{id}` | Чек покупки |
| `GET` | `/health` | Состояние сервиса, базы и курса |

Пример покупки:

```bash
curl -i http://localhost:8080/api/purchases \
  -H 'Content-Type: application/json' \
  -H 'X-User-Id: demo-user' \
  -H 'Idempotency-Key: demo-order-001' \
  -d '{"skinId":"11111111-1111-1111-1111-111111111111"}'
```

Повтор запроса с тем же `Idempotency-Key` у того же пользователя вернёт прежний чек. Попытка купить недоступный или удалённый скин даёт `409 Conflict`; ошибки чтения несуществующего ресурса — `404`. Ошибки валидации возвращаются в формате `application/problem+json`.

## Локальная разработка

Нужны Go 1.27 и Goose. Скопируйте пример окружения, затем запустите базу и миграции:

```bash
cp .env.example .env
docker compose up -d db
docker compose run --rm migrate
```

Загрузите env-переменные в текущий shell и запустите API:

```bash
set -a
source .env
set +a
HTTP_ADDR=:8081 go run ./cmd/api
```

При таком запуске API слушает `http://localhost:8081`. С Compose для фронтенда используется порт `8080`; порт можно переопределить через `WEB_PORT`.

Миграции можно применить вручную после установки Goose:

```bash
go install github.com/pressly/goose/v3/cmd/goose@v3.28.0
goose -dir migrations postgres "$DATABASE_URL" up
```

Параметры подключения, адрес CoinGecko и порт сервера задаются окружением. `DATABASE_URL` обязателен для запуска бинарного приложения; Compose сам собирает внутренний URL для контейнера API.

## Тесты и проверки

```bash
go test ./...
go test -race ./...
go vet ./...
test -z "$(gofmt -l .)"
```

Интеграционный тест PostgreSQL пропускается, если не задан `TEST_DATABASE_URL`. Чтобы запустить его, примените миграции к отдельной тестовой базе и укажите URL:

```bash
TEST_DATABASE_URL='postgres://minecraftskins:minecraftskins@localhost:5432/minecraftskins?sslmode=disable' go test ./tests/integration -v
```

GitHub Actions проверяет форматирование, `go vet`, тесты с race detector, PostgreSQL-интеграцию, сборку API и Docker runtime-образ.

## Архитектурные решения

- **`net/http` вместо роутер-фреймворка.** Маршрутов немного, встроенный `ServeMux` покрывает нужный контракт.
- **SQL через `pgx`.** Запросы, индексы и транзакция покупки остаются видимыми, без скрытого поведения ORM.
- **Goose для версионирования схемы.** Миграции применяются отдельным шагом до запуска API.
- **Интерфейсы только на границах.** Сервисы зависят от контрактов хранилища и провайдера курса, а не от деталей HTTP или PostgreSQL.
- **Денежные значения — decimal.** `float64` не подходит для воспроизводимых сумм в чеке.
- **Ключ идемпотентности scoped по `BuyerId`.** Повтор ключа одним пользователем возвращает его чек, одинаковые ключи разных пользователей не пересекаются.
- **Один backend в Compose.** Исходные пять копий API и отдельные Prometheus/Loki/Grafana удалены: для данного учебного сервиса они требовали больше инфраструктуры, чем решали задач.

## Что можно развить дальше

- Заменить демо-заголовок `X-User-Id` на настоящую аутентификацию и авторизацию.
- Добавить загрузку Minecraft-скинов и проверку формата/размера изображений.
- Добавить отдельные интеграционные тесты для конкурентных запросов покупки.
- Добавить метрики Prometheus и трассировку, если появится задача их использовать.
- Сгенерировать интерактивную Swagger UI по OpenAPI-спецификации.

## Источник

Исходный проект: [fairwix/MinecraftSkins](https://github.com/fairwix/MinecraftSkins). Фронтенд перенесён без изменения прикладного контракта. TLS private key и небезопасные тестовые endpoints исходного репозитория в Go-репозиторий не включены.
