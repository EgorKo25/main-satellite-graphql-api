# Main + Satellite GraphQL API

Go-сервис с PostgreSQL: один `Main` владеет ровно одним спутником `Tool`, `Table` или `Chair`. Создание, чтение, изменение и мягкое удаление доступны через `http://localhost:8080/graphql`. Спутник возвращается как GraphQL union и изменяется только вместе с владельцем.

Репозиторий: [EgorKo25/main-satellite-graphql-api](https://github.com/EgorKo25/main-satellite-graphql-api).

## Быстрый запуск в Docker

Нужны Docker Engine / Docker Desktop с Linux-контейнерами и Docker Compose v2. Go на хосте для этого способа не требуется.

```sh
git clone https://github.com/EgorKo25/main-satellite-graphql-api.git
cd main-satellite-graphql-api
cp .env.example .env
cp config.example.yaml config.yaml
docker compose up --build -d
docker compose ps -a
docker compose logs migrate api
```

В PowerShell вместо `cp` используйте `Copy-Item .env.example .env` и `Copy-Item config.example.yaml config.yaml`.

Без пользовательских настроек достаточно `docker compose up --build`: Compose использует значения по умолчанию и `config.example.yaml`. Если скопировали `.env.example`, создайте также `config.yaml`: в примере `.env` именно он указан в `CONFIG_PATH`.

Compose ждёт готовности PostgreSQL, запускает отдельный контейнер миграций, затем запускает API только после успешного завершения миграций. Повторный `up` миграций безопасен: применённые версии учитывает Goose. Такой порядок задаётся через [`depends_on` и условия готовности](https://docs.docker.com/compose/how-tos/startup-order/).

Проверка работающего API:

```sh
curl http://localhost:8080/graphql -H "Content-Type: application/json" --data '{"query":"query { main { id title } }"}'
```

В PowerShell:

```powershell
$body = @{ query = 'query { main { id title } }' } | ConvertTo-Json
Invoke-RestMethod -Uri http://localhost:8080/graphql -Method Post -ContentType 'application/json' -Body $body
```

На новой БД результат — `{"data":{"main":[]}}`. Встроенного браузерного редактора запросов нет; можно использовать любой GraphQL-клиент с этим URL. Introspection включена.

Остановка сохраняет данные в именованном томе `postgres-data`:

```sh
docker compose down
```

Пароли в `.env.example` — открытые учебные значения. API и PostgreSQL публикуют порты только на `127.0.0.1`. `.env` не должен попадать в Git; приложение не выводит DSN в лог. В Compose подключение API формируется из `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB` и адреса `db:5432`. Если пароль содержит специальные символы URL, при составлении DSN их нужно кодировать.

## Локальный запуск Go

Нужен Go 1.26.8 или новее. Для `-race` нужен поддерживаемый C-компилятор; Docker-способ тестирования уже содержит необходимые инструменты. Зависимости закреплены в `go.mod` и `go.sum`: gqlgen `v0.17.95`, pgx `v5.11.0`, goose `v3.28.0`; образы сборки и БД — `golang:1.26.8-bookworm` и `postgres:17.9-bookworm`.

Создайте локальный YAML и запустите только БД:

```sh
cp config.example.yaml config.yaml
docker compose up -d --wait db
```

Для миграций используется официальный goose CLI, закреплённый на `v3.28.0`. Build tags исключают ненужные этому проекту драйверы БД.

Bash / zsh:

```sh
export DATABASE_URL='postgres://graphql:graphql_dev@localhost:5432/graphql?sslmode=disable'
GOOSE_TAGS='no_clickhouse,no_libsql,no_mssql,no_mysql,no_sqlite3,no_vertica,no_ydb'
(cd migrations && go run -tags="$GOOSE_TAGS" github.com/pressly/goose/v3/cmd/goose@v3.28.0 -env=none -dir . -timeout 1m postgres "$DATABASE_URL" up)
go run ./cmd/api
```

PowerShell:

```powershell
Copy-Item config.example.yaml config.yaml
$env:DATABASE_URL = 'postgres://graphql:graphql_dev@localhost:5432/graphql?sslmode=disable'
$gooseTags = 'no_clickhouse,no_libsql,no_mssql,no_mysql,no_sqlite3,no_vertica,no_ydb'
Push-Location migrations
go run "-tags=$gooseTags" github.com/pressly/goose/v3/cmd/goose@v3.28.0 -env=none -dir . -timeout 1m postgres "$env:DATABASE_URL" up
Pop-Location
go run ./cmd/api
```

Go-процесс читает YAML из `CONFIG_PATH` (по умолчанию `config.yaml`); `DATABASE_URL` из окружения переопределяет `database.url`. Файл `.env` автоматически читает Compose, но не локальный Go-процесс. Примеры соответствуют стандартным настройкам `.env.example`. При изменении `DB_PORT` или учётных данных обновите локальный `DATABASE_URL`. Для одновременного запуска контейнерного и локального API задайте локальному процессу отдельный YAML с `http.addr: "0.0.0.0:8081"`.

Сборка бинарного файла:

```sh
go build -o bin/api ./cmd/api
```

Для Windows можно выбрать имя `bin/api.exe`. В Docker-образе находятся `/api`, официальный `/goose` и SQL-файлы `/migrations`. Контейнер миграций запускается из `/migrations` с `-dir .`; приложение использует `/config.yaml`. Compose монтирует выбранный YAML только для чтения. Образ запускается от UID/GID `65532:65532` и не требует shell во время работы. API завершает текущие запросы при SIGINT/SIGTERM в пределах `http.shutdown_timeout`, затем закрывает пул БД.

### Конфигурация

Конфигурация приложения — вложенный YAML. `config.Load(path)` возвращает проверенный `*config.App`; процесс хранит его в локальной переменной `cfg`, передаёт `cfg.Database` конструктору БД и использует `cfg.HTTP` при настройке сервера. Глобального изменяемого объекта конфигурации и геттеров нет.

Пример без секретов находится в [config.example.yaml](config.example.yaml). Загрузчик отклоняет неизвестные поля, несколько YAML-документов и значения, не прошедшие `go-playground/validator/v10`. Загрузка и проверка завершаются до открытия HTTP-сервера. Локальный `config.yaml` и `.env` исключены из Git.

| Поле YAML | Значение в примере | Назначение |
| --- | --- | --- |
| `database.url` | пустая строка | DSN; задайте здесь или через `DATABASE_URL` |
| `database.connect_timeout` | `5s` | Подключение и проверка доступности БД при старте |
| `database.max_conns` | `10` | Максимум соединений пула |
| `database.min_conns` | `0` | Минимум соединений, не выше `max_conns` |
| `http.addr` | `0.0.0.0:8080` | Адрес HTTP-сервера |
| `http.request_timeout` | `10s` | Общий контекст запроса, включая операции БД |
| `http.read_header_timeout` | `5s` | Чтение HTTP-заголовков |
| `http.read_timeout` | `10s` | Чтение HTTP-запроса |
| `http.write_timeout` | `15s` | Запись HTTP-ответа |
| `http.idle_timeout` | `1m` | Ожидание следующего запроса в соединении |
| `http.shutdown_timeout` | `15s` | Завершение активных запросов при остановке |

HTTP-настройки имеют эти же defaults при отсутствии полей; явно заданный нулевой timeout отклоняется. Параметры БД задаются YAML, а `database.min_conns: 0` допустим. Интервалы записываются как Go duration: `500ms`, `5s`, `1m`.

| Переменная окружения | Назначение |
| --- | --- |
| `CONFIG_PATH` | Путь YAML для Go-процесса; в Compose — путь файла на хосте, монтируемого как `/config.yaml` |
| `DATABASE_URL` | Переопределение `database.url` для API; подключение для команд `make migrate-*` |
| `TEST_DATABASE_URL` | Административное подключение к отдельному серверу тестовой БД |
| `HTTP_PORT` / `DB_PORT` / `TEST_DB_PORT` | Порты Compose на хосте: `8080` / `5432` / `5433` |
| `POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB` | Основная БД Compose: учебные `graphql` / `graphql_dev` / `graphql` |
| `MIGRATION_TIMEOUT` | Таймаут контейнера миграций, по умолчанию `1m` |

Compose задаёт контейнеру миграций `GOOSE_DRIVER=postgres` и `GOOSE_DBSTRING` с адресом `db:5432`. Параметры HTTP и пула меняются через YAML. Размер HTTP body ограничен 1 MiB, сложность GraphQL-запроса — 10000; стоимость списка учитывает `limit`.

## Контракт API

Полная исполняемая схема: [internal/graph/schema.graphqls](internal/graph/schema.graphqls).

```graphql
type Query {
  main(id: ID, limit: Int! = 20, offset: Int! = 0): [Main!]!
}

type Mutation {
  main(input: MainMutationInput!): MainMutationPayload
}

input MainMutationInput @oneOf {
  create: MainCreateInput
  update: MainUpdateInput
  delete: MainDeleteInput
}

union Satellite = Tool | Table | Chair

enum ChairType {
  abc
  cde
}
```

`Query.main` возвращает список активных записей по возрастанию `id`. Без `id` действуют `limit=20`, `offset=0`; допустимы `1 <= limit <= 100` и `offset >= 0`. Фильтр `id` также возвращает список; для неизвестной или удалённой записи это `[]`. Пагинация применяется и при фильтре по ID.

ID — положительный PostgreSQL `BIGINT`, максимум `9223372036854775807`. В ответе GraphQL ID всегда строка. В variables рекомендуется строка, чтобы JavaScript не потерял точность больших чисел. Ноль, отрицательные значения, знак `+`, дроби, произвольный текст и переполнение отклоняются. Времена представлены строками RFC 3339 в UTC; SQL `update_at` отображается в GraphQL как `updatedAt`.

`MainMutationPayload` содержит `main` и `deletedId`. Создание и изменение возвращают `main` с актуальным спутником и `deletedId: null`. Удаление возвращает `main: null` и строковый `deletedId`.

### OneOf: ровно один переданный ключ

`MainMutationInput`, `SatelliteCreateInput` и `SatelliteUpdateInput` используют `@oneOf`: должен быть передан ровно один ключ, и его значение должно быть ненулевым. Считается наличие ключа, включая ключи со значением `null`. Правило проверяется и для литералов в GraphQL-документе, и для JSON variables. Это семантика [OneOf Input Objects из спецификации GraphQL](https://spec.graphql.org/September2025/#sec-OneOf-Input-Objects).

| Вход | Результат |
| --- | --- |
| `{"delete":{"id":"42"}}` | Корректная форма |
| `{}` | Ошибка: нет выбранной операции |
| `{"create":null}` | Ошибка: выбранное значение `null` |
| `{"delete":{"id":"42"},"create":null}` | Ошибка: переданы два ключа |
| `{"tool":{},"table":{}}` внутри `satellite` | Ошибка: переданы два вида спутника |

Разновидность спутника выбирается при создании и затем не меняется. Попытка обновить `table` у владельца `tool` приводит к `SATELLITE_TYPE_MISMATCH`. При этом поле `Chair.type` разрешено изменять между `abc` и `cde`.

### Частичное изменение и `null`

Отсутствующее поле сохраняет значение. Явный `null` очищает только nullable-описание спутника. Это различие хранится в полях входных типов PostgreSQL через готовый `graphql.Omittable`; доменные объекты не содержат patch-состояний. См. [подход gqlgen к частичным обновлениям](https://gqlgen.com/reference/changesets/).

| Поле в update | Не передано | `null` | Значение |
| --- | --- | --- | --- |
| `title` | Сохранить | Ошибка | Заменить; `""` допустима |
| `satellite` | Сохранить | Ошибка | Изменить существующий спутник |
| `description1/2/3` | Сохранить | Очистить до SQL `NULL` | Заменить; `""` допустима |
| `chair.type` | Сохранить | Ошибка | `abc` или `cde` |

Update только с `id` отклоняется. Пустой patch спутника, например `{"tool":{}}`, тоже отклоняется, даже если одновременно передан новый `title`. При создании пустое описание можно не указывать или передать как `null`; для `Chair` всегда нужен `type`.

### Ошибки и удаление

Ошибки возвращаются в стандартном GraphQL-массиве `errors`. Для ошибок приложения используется `errors[].extensions.code`:

| Код | Ситуация |
| --- | --- |
| `BAD_USER_INPUT` | Неверный ID, пагинация, пустой patch, запрещённый `null` |
| `NOT_FOUND` | Изменение неизвестного/удалённого `Main` или удаление неизвестного |
| `ALREADY_DELETED` | Повторное удаление существующего удалённого `Main` |
| `SATELLITE_TYPE_MISMATCH` | Попытка заменить разновидность спутника |
| `INTERNAL_SERVER_ERROR` | Внутренняя ошибка; клиенту не выдаются SQL и детали инфраструктуры |

Ошибки синтаксиса и типизации отклоняются GraphQL-движком. Входная граница дополнительно проверяет OneOf в variables, неизвестные поля и точное написание enum до выполнения операций БД. Ошибки этих проверок имеют код `BAD_USER_INPUT`; стандартные ошибки parsing/validation могут иметь собственные сообщения и коды библиотеки. Проверяйте `errors`, даже если HTTP-статус равен 200.

Удаление мягкое: `deleted_at` владельца и спутника устанавливается в одно время в одной транзакции. Записи остаются в БД; активное чтение их скрывает. Восстановление не предусмотрено. Повторное удаление не обновляет timestamps и возвращает `ALREADY_DELETED`.

## Примеры запросов с variables

Для всех мутаций ниже используется один документ. В клиенте вставьте его в Query и соответствующий JSON — в Variables. ID в примерах условные: после создания подставьте фактически полученный `main.id`.

```graphql
mutation ChangeMain($input: MainMutationInput!) {
  main(input: $input) {
    main {
      id
      title
      createdAt
      updatedAt
      deletedAt
      satellite {
        __typename
        ... on Tool { id description1 createdAt updatedAt deletedAt }
        ... on Table { id description2 createdAt updatedAt deletedAt }
        ... on Chair { id description3 type createdAt updatedAt deletedAt }
      }
    }
    deletedId
  }
}
```

`Satellite` — union: поля конкретного спутника запрашиваются через inline fragments `... on Tool`, `... on Table`, `... on Chair`; `__typename` показывает выбранный тип.

### Создать каждый вид спутника

Tool:

```json
{"input":{"create":{"title":"Молоток","satellite":{"tool":{"description1":"Для мастерской"}}}}}
```

Table:

```json
{"input":{"create":{"title":"Рабочий стол","satellite":{"table":{"description2":"Дубовая столешница"}}}}}
```

Chair:

```json
{"input":{"create":{"title":"Офисное кресло","satellite":{"chair":{"description3":"С подлокотниками","type":"abc"}}}}}
```

Tool без описания, с допустимым пустым заголовком:

```json
{"input":{"create":{"title":"","satellite":{"tool":{}}}}}
```

### Прочитать список и запись по ID

```graphql
query ReadMain($id: ID, $limit: Int! = 20, $offset: Int! = 0) {
  main(id: $id, limit: $limit, offset: $offset) {
    id
    title
    createdAt
    updatedAt
    deletedAt
    satellite {
      __typename
      ... on Tool { id description1 }
      ... on Table { id description2 }
      ... on Chair { id description3 type }
    }
  }
}
```

Первая страница:

```json
{"limit":20,"offset":0}
```

Следующая страница:

```json
{"limit":20,"offset":20}
```

По ID:

```json
{"id":"1"}
```

Для отсутствующего или мягко удалённого ID ожидается `{"data":{"main":[]}}`.

### Обновить `title`

Спутник сохраняется:

```json
{"input":{"update":{"id":"1","title":"Новый заголовок"}}}
```

### Обновить поля спутника

Tool:

```json
{"input":{"update":{"id":"1","satellite":{"tool":{"description1":"Новое описание инструмента"}}}}}
```

Table:

```json
{"input":{"update":{"id":"2","satellite":{"table":{"description2":"Новое описание стола"}}}}}
```

Chair, включая допустимую смену `type`:

```json
{"input":{"update":{"id":"3","satellite":{"chair":{"description3":"Новое описание кресла","type":"cde"}}}}}
```

Заголовок и спутник в одной транзакции:

```json
{"input":{"update":{"id":"1","title":"Новый молоток","satellite":{"tool":{"description1":"Обновлено вместе с владельцем"}}}}}
```

### Очистить описание явным `null`

Tool:

```json
{"input":{"update":{"id":"1","satellite":{"tool":{"description1":null}}}}}
```

Table:

```json
{"input":{"update":{"id":"2","satellite":{"table":{"description2":null}}}}}
```

Chair, сохранив его `type`:

```json
{"input":{"update":{"id":"3","satellite":{"chair":{"description3":null}}}}}
```

Ответ содержит соответствующее описание `null`. Передача `""` вместо `null` сохранит пустую строку.

### Мягко удалить и повторить удаление

```json
{"input":{"delete":{"id":"1"}}}
```

Первый ответ:

```json
{"data":{"main":{"main":null,"deletedId":"1"}}}
```

Повторите те же variables: результат содержит ошибку `ALREADY_DELETED`, а `data.main` равен `null`. Чтение этого ID возвращает `[]`; его изменение приводит к `NOT_FOUND`.

### Ошибочные операции

Каждый JSON — отдельный набор variables к `ChangeMain`:

```json
{"input":{"create":{"title":"Некорректный выбор","satellite":{"tool":{},"table":{}}}}}
```

Два вида спутника: ошибка OneOf.

```json
{"input":{"delete":{"id":"2"},"create":null}}
```

Две операции, включая явно переданный `null`: ошибка OneOf.

```json
{"input":{"update":{"id":"3","title":null}}}
```

Запрещённый `null`: `BAD_USER_INPUT`.

```json
{"input":{"update":{"id":"3"}}}
```

Пустое изменение: `BAD_USER_INPUT`.

```json
{"input":{"update":{"id":"2","satellite":{"table":{}}}}}
```

Пустой patch спутника: `BAD_USER_INPUT`.

```json
{"input":{"update":{"id":"2","satellite":{"chair":{"type":"abc"}}}}}
```

Если `2` владеет `Table`, результат — `SATELLITE_TYPE_MISMATCH`.

```json
{"input":{"delete":{"id":"0"}}}
```

Неверный ID: `BAD_USER_INPUT`.

```json
{"input":{"update":{"id":"9223372036854775807","title":"Не существует"}}}
```

Для неизвестного ID: `NOT_FOUND`.

```json
{"input":{"create":{"title":"Кресло","satellite":{"chair":{"type":"unknown"}}}}}
```

Неверный enum отклоняется GraphQL-движком. Для документа `ReadMain` variables `{"limit":0}` или `{"offset":-1}` дают `BAD_USER_INPUT`.

Пример полного HTTP body, который можно сохранить как UTF-8 `request.json`:

```json
{
  "query":"mutation ChangeMain($input: MainMutationInput!) { main(input: $input) { main { id title satellite { __typename ... on Tool { description1 } } } deletedId } }",
  "variables":{"input":{"create":{"title":"Молоток","satellite":{"tool":{"description1":"Для мастерской"}}}}}
}
```

```sh
curl http://localhost:8080/graphql -H "Content-Type: application/json" --data-binary @request.json
```

В PowerShell для последней команды используйте `curl.exe`, чтобы вызвать curl, а не alias PowerShell.

## Хранение и транзакции

Схема БД находится в [migrations/00001_main_satellites.sql](migrations/00001_main_satellites.sql).

| Таблица | Основные поля |
| --- | --- |
| `main` | `id`, `title`, `sub_id`, `sub_obj`, `created_at`, `update_at`, `deleted_at` |
| `tools` | `id`, `main_id`, `description1`, `created_at`, `update_at`, `deleted_at` |
| `tables` | `id`, `main_id`, `description2`, `created_at`, `update_at`, `deleted_at` |
| `chairs` | `id`, `main_id`, `description3`, `type`, `created_at`, `update_at`, `deleted_at` |

ID — `BIGINT` с identity; даты — `TIMESTAMPTZ`; `type` — PostgreSQL enum `chair_type ('abc', 'cde')`. Все колонки, кроме описаний и `deleted_at`, обязательны. `sub_obj` ограничен значениями `tools`, `tables`, `chairs`. `satellite.main_id` имеет `UNIQUE` и внешний ключ на `main.id`; активное чтение использует частичный индекс по `main.id`.

```mermaid
flowchart LR
    Client[GraphQL client] --> Graph[GraphQL schema and resolvers]
    Graph --> Database[postgres.DB: Main operations and transactions]
    Database --> Main[(main)]
    Database --> Satellites[(tools / tables / chairs)]
```

`main.sub_obj + main.sub_id` указывают на спутник; его `main_id` указывает обратно. После проверки OneOf GraphQL-резолвер один раз выбирает конкретный метод `DB.CreateTool`, `DB.UpdateChair` и аналогичные. Для soft delete разновидность становится известна из заблокированной строки Main; соответствующий SQL-метод выбирается через таблицу диспетчеризации, созданную в конструкторе DB. Она хранит методы без данных запроса. Пользовательские значения передаются SQL-параметрами; названия таблиц фиксированы в запросах.

Полиморфная ссылка `main.sub_id` не представлена одним внешним ключом на три таблицы. Целостность пары и единственность вида обеспечиваются транзакциями приложения; `UNIQUE(main_id)` действует внутри каждой таблицы спутников. Произвольная запись SQL в обход операций приложения может нарушить общую полиморфную связь — это граница данного решения.

Объект `postgres.DB` владеет пулом соединений. Каждый метод записи создаёт локальную транзакцию; её не передают наружу и не хранят в общем DB. Создание владельца и спутника атомарно: сначала выделяется ID спутника из sequence, затем вставляются Main с заполненным `sub_id` и спутник с заполненным `main_id`. Update/delete блокируют Main через `SELECT FOR UPDATE`; после получения блокировки проверяются состояние удаления и допустимость выбранной операции. Последующий `UPDATE` спутника блокирует его строку. Все SQL-запросы одной операции выполняются через ту же транзакцию. Результат читается до commit и возвращается только после успешного commit; ошибки записи и commit не становятся успешным ответом. Отложенный rollback использует отдельный ограниченный контекст. Конкурентные операции над одним владельцем последовательно получают блокировку, поэтому не создают частично обновлённую пару. Пропуски identity после rollback допустимы.

Любой корректный update обновляет `Main.updatedAt`. `satellite.updatedAt` обновляется только при передаче patch спутника; изменение одного `title` сохраняет время спутника. При совместном изменении и при удалении используется одно значение времени для обеих записей. `createdAt` не меняется.

Чтение страницы загружает владельцев вместе со спутниками одним SQL-запросом с соединениями. Резолвер union читает уже загруженные данные, поэтому число SQL-запросов не растёт с количеством `Main` на странице.

### Структура проекта

```text
cmd/api/                  запуск HTTP-сервера и graceful shutdown
internal/config/          типизированный YAML, env override DSN и проверка настроек
internal/domain/          Main, Satellite, Tool, Table и Chair
internal/postgres/        пул, входные типы операций, транзакции, SQL и чтение
internal/graph/           SDL, выбор операции, проверка GraphQL-входа и HTTP-handler
internal/graph/scalar/    ID с представлением строкой и Time в UTC
internal/graph/model/     сгенерированные транспортные контейнеры OneOf и payload
internal/graph/generated/ сгенерированный исполняемый GraphQL-код
migrations/               обычные SQL-файлы goose up/down
config.example.yaml       пример конфигурации без секретов
```

`Satellite` содержит общие идентификаторы и timestamps и встроен в `Tool`, `Table`, `Chair`. У `Chair` находятся только его собственные `description3` и `type`. Интерфейс `domain.SubObject` даёт доступ к разновидности и общим данным для смешанного результата чтения.

В [gqlgen.yml](gqlgen.yml) выходные Main/Tool/Table/Chair связаны напрямую с доменными сущностями, а конкретные входы создания и обновления — с типами операций PostgreSQL. gqlgen заполняет эти входы при разборе запроса; отдельной цепочки mapper/DTO между слоями нет. OneOf-контейнеры остаются транспортными типами GraphQL. `graphql.Omittable` во входах PostgreSQL — сознательная зависимость от готового механизма gqlgen для трёх состояний поля.

Бизнес-проверки текущего состояния и границы транзакций находятся в операциях `postgres.DB`; SQL с аргументами записан рядом с вызовом pgx. Отдельного слоя из методов, только перенаправляющих вызовы в DB, нет. Внутренние Go-методы для конкретных разновидностей не создают самостоятельных GraphQL-операций для сателлитов.

### Миграции

Используется официальный [goose CLI](https://github.com/pressly/goose) `v3.28.0`, без собственного runner и без `embed`. Goose читает SQL-файлы из каталога `migrations` и ведёт версии в служебной таблице БД. Команды запускаются из этого каталога с `-dir .`; `-env=none` отключает неявное чтение dotenv самим goose. Версия и build tags CLI одинаковы в Makefile и Dockerfile.

API сам миграции не запускает. Обычный Compose сначала выполняет `up` в отдельном контейнере, затем запускает API. Повторный запуск не создаёт таблицы заново и не удаляет данные.

Для локального запуска в окружении с Make доступны:

```sh
make migrate-status
make migrate-up
```

Эти команды используют `DATABASE_URL` из окружения и тот же официальный CLI, что показан выше. `down` удаляет доменную схему вместе с данными: перед проверкой отката переключите `DATABASE_URL` на заранее созданную одноразовую БД.

```sh
make migrate-down
make migrate-up
```

Без Make используйте приведённую выше команду goose, заменив последний аргумент `up` на `status` или `down`. Интеграционная проверка `down/up` выполняется только внутри собственной временной БД; обычный запуск не откатывает основную схему.

## Тестирование

Unit-тесты и статические проверки:

```sh
go test -count=1 ./...
go vet ./...
go build ./...
golangci-lint run --build-tags=integration
```

Используйте golangci-lint второй версии, совместимый с Go 1.26.8. Если среда поддерживает CGO и race detector, дополнительно выполните `go test -race -count=1 ./...`; контейнерный запуск ниже включает эту проверку.

Воспроизведение генерации gqlgen и моков mockgen:

```sh
go generate ./...
```

Версии генераторов закреплены в `go.mod`. Сгенерированные файлы включены в репозиторий, поэтому для обычной сборки генерация не требуется. После повторного `go generate ./...` в них не должно появляться изменений.

### Интеграционные тесты в Docker

```sh
docker compose --profile test run --build --rm test
```

Команда запускает отдельный `test-db` и выполняет `go test -race -tags=integration -count=1 ./...` внутри Go-контейнера. Основные `db`, `migrate`, `api` не нужны. `test-db` имеет отдельные учётные данные, порт и tmpfs вместо постоянного тома. Он не использует `postgres-data` основной БД.

После тестов можно остановить только тестовый PostgreSQL:

```sh
docker compose --profile test stop test-db
```

Его данные временные и не сохраняются после остановки контейнера.

### Интеграционные тесты с Go на хосте

```sh
docker compose --profile test up -d --wait test-db
```

Bash / zsh:

```sh
export TEST_DATABASE_URL='postgres://graphql_test:graphql_test@localhost:5433/postgres?sslmode=disable'
go test -race -tags=integration -count=1 ./...
```

PowerShell:

```powershell
$env:TEST_DATABASE_URL = 'postgres://graphql_test:graphql_test@localhost:5433/postgres?sslmode=disable'
go test -race -tags=integration -count=1 ./...
```

`TEST_DATABASE_URL` — административное подключение к выделенному тестовому серверу. Пользователь должен иметь право `CREATE DATABASE`. Каждый запуск создаёт БД с уникальным именем, применяет миграции и удаляет только созданную им БД. Проверка `down/up` выполняется внутри собственной тестовой БД. Тесты не перебирают и не удаляют чужие БД или контейнеры.

Проверяются операции через настоящий GraphQL HTTP-handler и PostgreSQL: все разновидности спутников; union; ошибки OneOf в литералах и variables; пропущенные и `null`-поля; неизменяемость вида спутника; пагинация и ID; мягкое удаление; откат транзакций при сбое; конкурентные изменения; отсутствие N+1 по SQL-tracer. Unit-тесты покрывают валидацию, конфигурацию и GraphQL HTTP-контракт с generated mock базы данных. Mock проверяет значимые вызовы и переданные типизированные значения; SQL, блокировки и атомарность проверяются настоящим PostgreSQL.

Эквивалентные короткие команды доступны в [Makefile](Makefile): `make build`, `make run`, `make generate`, `make fmt`, `make test`, `make test-race`, `make test-integration`, `make test-docker`, `make vet`, `make lint`, `make up`, `make down`, `make migrate-status`, `make migrate-up`. `make migrate-down` явно откатывает последнюю миграцию выбранной БД.

## Требования задания и принятые решения

### Требования заказчика и его уточнения

- Go, PostgreSQL, структура БД создаётся миграциями.
- Четыре доменные таблицы `main`, `tools`, `tables`, `chairs` с заданными столбцами, включая `update_at`; ссылки `sub_id`, `sub_obj`, `main_id`; enum `abc/cde` у chairs.
- Одно корневое поле Query и одно Mutation для создания, чтения, изменения и удаления через Main; отдельного API сателлитов нет.
- Чтение учитывает `deleted_at`; soft delete Main помечает также его сателлит.
- Восстановление запрещено; повторное удаление возвращает ошибку.
- Переключать Main между Tool/Table/Chair нельзя; изменяются поля выбранной разновидности.

### Принятые проектные решения

Эти решения дополняют исходное условие и зафиксированы в техническом контракте проекта; они не приписываются заказчику как отдельные требования.

- У Main ровно один сателлит; на выходе используется union `Satellite`. Клиент передаёт содержимое сателлита, а ID, ссылки и timestamps задаёт сервер.
- Один HTTP endpoint `/graphql`; `@oneOf` выбирает операцию и разновидность сателлита. Частичный update различает отсутствие поля, null и значение.
- `Query.main` возвращает список, в том числе при фильтре по ID; отсутствующий или удалённый ID даёт `[]`.
- Пагинация по `limit/offset`, предел 100, порядок `id ASC`.
- ID — положительный `BIGINT`, GraphQL-представление — строка.
- Описания необязательны при создании; null при обновлении очищает описание. `title` можно менять, пустые строки допустимы; пустые update и patch спутника отклоняются.
- Изменение удалённой записи возвращает `NOT_FOUND`, повторное удаление — `ALREADY_DELETED`.
- `Chair.type` можно изменять; разновидность `Tool/Table/Chair` сохраняется.
- Полиморфная связь проверяется приложением; прямой SQL в обход него не входит в контракт целостности.
- HTTP-handler gqlgen с прямыми bindings, один объект `postgres.DB` с пулом и локальными транзакциями, SQL без ORM, официальный goose CLI, стандартный `slog`.
- Типизированная YAML-конфигурация, передаваемая из `main`; DSN допускает env override.
- Docker Compose, проверки контракта и интеграционные тесты с настоящим PostgreSQL.
- Introspection доступна; HTTP body, сложность запроса и время исполнения ограничены.

API не содержит отдельных операций для спутников, REST-маршрутов, subscriptions, восстановления удалённых записей, авторизации или брокеров сообщений. Каждый вызов поля мутации выполняется в собственной транзакции; несколько полей в одном GraphQL-документе не объединяются в одну общую транзакцию.

## Ссылки

- [GraphQL September 2025: OneOf Input Objects](https://spec.graphql.org/September2025/#sec-OneOf-Input-Objects).
- [gqlgen: Omittable и частичные изменения](https://gqlgen.com/reference/changesets/).
- [Docker Compose: порядок запуска и ожидание миграций](https://docs.docker.com/compose/how-tos/startup-order/).
- [Официальный образ Go](https://hub.docker.com/_/golang) и [официальный образ PostgreSQL](https://hub.docker.com/_/postgres).
