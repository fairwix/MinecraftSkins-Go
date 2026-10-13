<div align="center">

# 🎮 MinecraftSkins

### Каталог и магазин Minecraft-скинов с динамическими ценами в BTC

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![API](https://img.shields.io/badge/API-REST-2496ED)](api/openapi.yaml)
[![CI](https://github.com/fairwix/MinecraftSkins-Go/actions/workflows/ci.yml/badge.svg)](https://github.com/fairwix/MinecraftSkins-Go/actions/workflows/ci.yml)

**MinecraftSkins — backend-сервис для каталога, ценообразования и покупки игровых скинов.**
API написан на Go, хранит данные в PostgreSQL и пересчитывает цены с учётом курса BTC/USD.

</div>

## О проекте

Сервис объединяет каталог скинов и покупки в небольшой, но цельный backend. Пользователь может искать и фильтровать позиции, видеть цену по актуальному курсу и оформлять покупку. Чек сохраняет цену и курс на момент операции, поэтому история заказов остаётся неизменной при последующих колебаниях рынка.

Проект демонстрирует практические решения для Go backend-разработки: HTTP API на стандартной библиотеке, разделение прикладной логики и доступа к данным, транзакции, идемпотентность, интеграцию с внешним API, миграции базы и запуск окружения через Docker Compose.

В демонстрационном сценарии идентификатор покупателя передаётся заголовком `X-User-Id`.

## Возможности

- Каталог с поиском, фильтром доступности и пагинацией.
- Создание, редактирование и мягкое удаление скинов.
- Динамическая цена с десятичной арифметикой и округлением до центов.
- Получение курса BTC/USD из CoinGecko с кэшем на 60 секунд и fallback на последнее успешное значение сроком до 10 минут.
- Покупка с сохранением цены и курса в чеке.
- Идемпотентная обработка покупок с заголовком `Idempotency-Key`.
- История покупок с фильтрами и постраничной выдачей.
- Health check, OpenAPI-спецификация и веб-интерфейс.

## Технологии

| Область | Технологии |
| --- | --- |
| Язык и HTTP | Go 1.27, `net/http`, `ServeMux` |
| База данных | PostgreSQL 16, `pgx/v5`, SQL |
| Миграции | [Goose](https://github.com/pressly/goose) |
| Денежные значения | `shopspring/decimal` |
| Конфигурация и логи | Переменные окружения, `log/slog` |
| Внешние интеграции | CoinGecko API, потокобезопасный кэш |
| Инфраструктура | Docker, Docker Compose, Nginx |
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

Запрос проходит через HTTP-обработчик, сервис приложения и хранилище. Обработчик отвечает за HTTP и JSON, сервисы — за правила каталога и покупки, PostgreSQL-слой — за SQL и транзакции. При покупке сервис блокирует строку скина, проверяет его доступность и сохраняет чек в одной транзакции.

```text
cmd/api/                 запуск приложения и graceful shutdown
internal/config/         конфигурация из переменных окружения
internal/domain/         модели, ошибки и правила ценообразования
internal/service/        сценарии каталога и покупки
internal/postgres/       параметризованные запросы и транзакции
internal/ratestore/      CoinGecko, кэш, fallback и объединение запросов
internal/httpapi/        маршруты, валидация и ответы Problem Details
migrations/              SQL-миграции Goose
api/openapi.yaml         описание REST API
frontend/                веб-интерфейс
```

### Расчёт цены

```text
Итоговая цена = базовая цена × (1 + курс BTC/USD ÷ 50 000)
```

Денежные значения рассчитываются через decimal-типы и округляются банковским методом до двух знаков после запятой.

## Быстрый запуск

Нужны Docker и Docker Compose.

```bash
cp .env.example .env
docker compose up --build
```

Compose запустит PostgreSQL, применит миграции Goose, поднимет API и раздаст веб-интерфейс через Nginx.

| Адрес | Назначение |
| --- | --- |
| <http://localhost:8080> | Веб-интерфейс |
| <http://localhost:8080/api/skins> | Каталог API |
| <http://localhost:8080/api/rates/btc-usd> | Курс BTC/USD |
| <http://localhost:8080/health> | Состояние API и зависимостей |
| [`api/openapi.yaml`](api/openapi.yaml) | Спецификация OpenAPI 3.1 |

Остановить сервисы, сохранив данные:

```bash
docker compose down
```

Удалить контейнеры и локальные данные базы:

```bash
docker compose down -v
```

## REST API

Полные схемы запросов и ответов приведены в [OpenAPI-спецификации](api/openapi.yaml).

| Метод | Маршрут | Назначение |
| --- | --- | --- |
| `GET` | `/api/skins` | Каталог: `availableOnly`, `search`, `skip`, `take`; общее число — в `X-Total-Count` |
| `GET` | `/api/skins/{id}` | Скин с актуальной ценой |
| `POST` | `/api/skins` | Создание скина |
| `PUT` | `/api/skins/{id}` | Обновление скина |
| `DELETE` | `/api/skins/{id}` | Мягкое удаление скина |
| `GET` | `/api/rates/btc-usd` | Курс, источник и возраст значения |
| `POST` | `/api/purchases` | Покупка; заголовки `X-User-Id` и `Idempotency-Key` |
| `GET` | `/api/purchases` | История: `mineOnly`, `skinId`, `from`, `to`, `skip`, `take` |
| `GET` | `/api/purchases/{id}` | Чек покупки |
| `GET` | `/health` | Состояние сервиса, базы данных и курса |

Пример покупки:

```bash
curl -i http://localhost:8080/api/purchases \
  -H 'Content-Type: application/json' \
  -H 'X-User-Id: demo-user' \
  -H 'Idempotency-Key: demo-order-001' \
  -d '{"skinId":"11111111-1111-1111-1111-111111111111"}'
```

Повтор запроса с тем же ключом идемпотентности для того же покупателя возвращает прежний чек. Покупка недоступного скина отвечает `409 Conflict`, запрос к отсутствующему ресурсу — `404`. Ошибки валидации возвращаются в формате `application/problem+json`.

## Локальная разработка

Для запуска API локально установите Go 1.27 и Goose. Скопируйте настройки окружения и запустите базу с миграциями:

```bash
cp .env.example .env
docker compose up -d db
docker compose run --rm migrate
```

Загрузите переменные окружения и запустите сервер:

```bash
set -a
source .env
set +a
HTTP_ADDR=:8081 go run ./cmd/api
```

API будет доступно на <http://localhost:8081>. Порт веб-интерфейса в Compose по умолчанию — `8080`; его можно изменить через `WEB_PORT`.

Миграции также можно применить вручную:

```bash
go install github.com/pressly/goose/v3/cmd/goose@v3.28.0
goose -dir migrations postgres "$DATABASE_URL" up
```

Параметры подключения к базе, адрес CoinGecko и адрес HTTP-сервера задаются через окружение. Для запуска бинарного приложения требуется `DATABASE_URL`.

## Тесты и проверки

```bash
go test ./...
go test -race ./...
go vet ./...
test -z "$(gofmt -l .)"
```

Интеграционные тесты PostgreSQL используют отдельную тестовую базу и переменную `TEST_DATABASE_URL`:

```bash
TEST_DATABASE_URL='postgres://minecraftskins:minecraftskins@localhost:5432/minecraftskins?sslmode=disable' go test ./tests/integration -v
```

GitHub Actions проверяет форматирование, `go vet`, тесты с race detector, PostgreSQL-интеграцию, сборку API и Docker runtime-образ.

## Архитектурные решения

- **Стандартный `net/http`.** Встроенный `ServeMux` покрывает маршруты и снижает число внешних зависимостей.
- **SQL через `pgx`.** Запросы, индексы и транзакции остаются явными и проверяемыми.
- **Goose для схемы базы.** Версионированные SQL-миграции применяются отдельным шагом перед запуском API.
- **Интерфейсы на границах приложения.** Сервисы зависят от контрактов хранилища и поставщика курса.
- **Decimal для денег.** Точные десятичные значения сохраняются при расчёте и в чеке покупки.
- **Идемпотентность в рамках покупателя.** Повторный запрос не создаёт второй чек, а ключи разных покупателей независимы.
- **Транзакция на покупку.** Проверка доступности скина и запись чека выполняются согласованно.

## Возможные направления развития

- Подключить аутентификацию и авторизацию пользователей.
- Реализовать загрузку скинов и проверку формата и размера изображений.
- Добавить отдельные тесты конкурентных запросов покупки.
- Подключить метрики и распределённую трассировку.
- Добавить интерактивный Swagger UI для OpenAPI-спецификации.
