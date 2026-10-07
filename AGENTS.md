# Правила работы над проектом

Этот AGENTS.md хранится в Git. Он содержит проектные договорённости и правила четырёх skills: **go-code-style**, **pr-workflow**, **semver**, **glab**. Для работы на другом компьютере локальная установка skills не нужна. Последние явные указания пользователя и проектные исключения ниже имеют приоритет над общими правилами.

Перед изменением Go читай [go-code-style](#go-code-style); для коммитов, ревью и CI — [pr-workflow](#pr-workflow); при выборе версии — [semver](#semver); для GitLab CLI — [glab](#glab). Наличие инструкции само по себе не поручает рефакторинг, выпуск релиза или изменение удалённого состояния.

## Работа и ревью

- Все текущие изменения собраны в **PR #1: `feat/storage` → `main`**. PR #2–#6 заменены им. Продолжай исправления небольшими законченными коммитами в ветке PR #1, без параллельных версий тех же изменений.
- Пользователь разрешил коммитить и отправлять завершённые изменения согласованной задачи без ревью перед каждым коммитом. Ревью проходит в PR; **слияние в main выполняет пользователь**. Не переписывай опубликованную историю без отдельной договорённости.
- Перед работой проверь Git и существующие файлы; сохраняй полезный код и незакоммиченные изменения пользователя. Существующий спорный код не является образцом стиля. Новое замечание останавливает несогласованное расширение задачи.
- В отчёте отделяй сделанное от предложений; перечисляй фактические проверки и ограничения. Пропуск проверки не считается успехом. Повторно не запрашивай уже полученные разрешения.

## Конфигурация и запуск

- Конфигурация — YAML; в `main` заданы `var configPath = "./config/config.yaml"` и `fs := afero.NewOsFs()`, загрузка — `config.Load(fs, configPath)`. Передавай общую FS потребителям; не создавай её внутри загрузчика. Не вводи env overrides.
- `config.App` — тип, не глобальная переменная. Загрузчик возвращает объект; `main` создаёт основные зависимости, передавая разделы `cfg.Database`, `cfg.HTTP` конструкторам. Используй вложенные типизированные структуры и экспортированные поля без геттеров.
- `database.url`, `database.user`, `database.password` — отдельные поля. Учётные данные pgx получает из `cfg.Database.User/Password`, без подмены окружением или данными URL.
- `App` встраивает `Database`, `HTTP`, `Logger` с YAML-тегами вложенных разделов. Defaults — теги `default` и `creasty/defaults` до Decode; затем YAML и validator. Не затирай явные нули defaults после Decode. Сохраняй KnownFields; не добавляй `extra` и второй Decode.
- `config/config.yaml` локальный и исключён из Git. `config.example.yaml` и `config.compose.yaml` остаются в корне и содержат учебные настройки. Реальные секреты не попадают в примеры, инструкции и коммиты.
- **Логгер глобальный по решению пользователя:** `main` один раз вызывает `logger.Initialize(fs, cfg.Logger)` с той же FS, что передана в `config.Load`; компоненты используют `logger.Get(name)`. Не передавай логгер через конструкторы. Это исключение не делает конфигурацию глобальной.
- Адаптер логгера использует zap; выходы и их настройки описываются списком `logger.cores` в YAML. Сохраняй несколько настраиваемых выходов.
- Файловые выходы логгера работают через переданную `afero.Fs`. Проверяй запись, дозапись и несколько выходов на MemMapFs; ошибки открытия, записи, Sync и Close вводи на границе FS/File в тестах. MemMapFs сама не воспроизводит ошибки физического диска. Закрывай все открытые файлы даже при ошибке одного из них, сохраняя причины ошибок.
- Ошибка загрузки конфигурации — `panic` непосредственно в `main`. Сразу после получения логгера ставь `defer func() { _ = log.Close() }()`: пользователь разрешил игнорировать ошибку закрытия. Сразу после проверки ошибки `postgres.New` ставь `defer database.Close()`. Для ошибки подключения к БД или Serve используй `log.Error` и `return` по решению пользователя: defer выполняются, main возвращается с кодом 0. Не заменяй return на panic/Fatal или wrapper ради exit code и не подавляй gocritic.
- Не добавляй функцию `run` ради exit code. Обработка сигналов из `main` удалена по решению пользователя; используй `context.Background()`. SIGINT/SIGTERM завершают процесс без graceful shutdown и выполнения defer. В TestMain не вызывай os.Exit из defer: это скрывает panic.
- Создание HTTP-сервера, маршруты, таймауты, запуск и graceful shutdown при отмене переданного контекста принадлежат `internal/server.Server`. `main` создаёт зависимости и передаёт `cfg.HTTP` и готовый GraphQL-handler в `server.New`. Вызов и проверку ошибки объединяй: `if err = httpServer.Serve(ctx); err != nil`. Не возвращай настройку `http.Server` в `main`.

## Модель и PostgreSQL

- `Main` — корневой объект, сателлиты доступны только через него. Общие ID, MainID и timestamps находятся в `Satellite`, встроенном в `Tool`, `Table`, `Chair`. Описания принадлежат конкретным типам, ChairType — только Chair.
- Выходные GraphQL-типы связаны напрямую с domain. Входы сателлитов и их OneOf-контейнеры gqlgen разбирает в `map[string]any`. SDL задаёт допустимые поля и типы; не дублируй их реестром колонок в Go. Промежуточный service удалён; не возвращай слои перенаправляющих методов и копии DTO/patch.
- Один `postgres.DB` хранит пул. `postgres.New(ctx, cfg.Database)` создаёт и проверяет соединение, `DB.Close()` закрывает пул. Операции — методы DB.
- Транзакция всей пары Main + сателлит локальна внутри операции DB: явные commit/rollback, наружу транзакция не передаётся, общий DB не хранит данные запроса. Все SQL операции используют её транзакцию.
- Rollback размещай непосредственно в defer каждой операции: вызов `transaction.Rollback(ctx)`, проверка ошибки кроме `pgx.ErrTxClosed` и логирование. Не возвращай метод DB.rollback и отдельный cleanup-контекст для этих операций. Разбор RETURNING сателлита выполняй на месте без collectSatellite.
- Используй общие `DB.Create`, `DB.Update`, `DB.Delete` для всей связки. Единственное соответствие имён — `tool → tools`, `table → tables`, `chair → chairs`. Не возвращай отдельные write-методы Tool/Table/Chair, диспетчеризацию callbacks и реестр полей. Имена полей входа совпадают с SQL-колонками; SQL-builder получает проверенные gqlgen maps, а не произвольный JSON.
- Сателлитные INSERT/UPDATE собирает Squirrel через SetMap. Имена таблиц выбираются только из фиксированного соответствия; значения передаются параметрами. Служебные поля добавляет DB в рабочую копию, входные maps не изменяются. Main и типизированные объекты ответа сохраняются.
- PostgreSQL возвращает сателлит как `row_to_json` в `Main.SatelliteData` (`json.RawMessage`): при записи через RETURNING выбранной таблицы, при чтении списка через одну команду с LATERAL UNION ALL после пагинации Main. JSON — формат результата запроса; четыре реляционные таблицы сохраняются. Не возвращай типизированные collectors сателлитов, nullableSatellite и повторное чтение агрегата через readTx. Main читается typed collector pgx.
- Единственный выбор Tool/Table/Chair находится в resolver `Main.satellite`: фиксированная таблица фабрик и стандартный `json.Unmarshal` в конкретный объект. JSON-теги описывают имена SQL-полей, без промежуточной map и ручного копирования. Проверки связности выполняются в DB независимо от selection set; JSON декодируется только при запросе satellite. Main timestamps декодируются pgx в UTC; timestamps сателлита разбирает `time.Time`, а GraphQL Time сериализует их в UTC.
- Валидатор PostgreSQL принадлежит экземпляру DB и создаётся один раз при инициализации. Не смешивай его с блоком sentinel errors и не создавай заново на каждый запрос. Пагинация проверяется по тегам ListInput непосредственно в List.
- SQL и аргументы держи рядом с Query/QueryRow/Exec. Не склеивай запросы из общих SQL-констант. Ошибку Exec и RowsAffected проверяй сразу на месте, без общего exactlyOne.
- Многострочный SQL: новая строка после открывающей обратной кавычки, отступ внутри литерала, `;` в конце запроса, закрывающая кавычка на отдельной строке. Короткие аргументы размещай рядом с ней в пределах лимита длины строки.
- Goose использует обычные SQL up/down-файлы в `migrations`. CLI запускается из этого каталога с путём `"."`. Не возвращай embed и собственный runner-обёртку с направлением.

## Контракт важнее упрощения

- Сохраняй публичный SDL, ровно одно бизнес-поле Query и одно Mutation, без прямого API сателлитов; столбец БД называется `update_at`.
- OneOf проверяет **присутствие ключей**, включая явно переданный null, для литералов и variables до изменений БД. Подсчёт ненулевых указателей этого не заменяет.
- У Main patch использует готовый `graphql.Omittable`; внутри map сателлита присутствие ключа различает отсутствие / null / значение. Учитывай типизированный nil после gqlgen coercion. Не вводи собственные Patch и `*Set`. Отсутствие сохраняет значение; null очищает описание, но запрещён для title, chair.type, satellite. Пустые строки допустимы.
- Defaults пагинации заданы SDL; на patch defaults запрещены. Пустой update и пустой patch сателлита отклоняются. Внешний клиент не задаёт служебные поля.
- ID — положительный BIGINT, на API строка. Формат и диапазон ID, включая запрет `+1`, и допустимые enum проверяются **GraphQL-входом**; PostgreSQL не дублирует эти проверки. В DB остаются patch, пагинация, состояние и полиморфная связность.
- `oneof=abc cde` валидатора проверяет значение enum, а не GraphQL @oneOf. `required` для строки не подходит для запрета только null при разрешённой пустой строке.
- Main блокируется первым через SELECT FOR UPDATE; состояние проверяется после получения блокировки. Soft delete согласован для пары, восстановления нет, повторное удаление — ошибка. Разновидность Tool/Table/Chair не меняется; chairs.type менять можно.
- Время операции бери один раз через `time.Now().UTC()`, для update/delete после блокировки Main. Передай его обеим строкам; не запрашивай время у PostgreSQL и не оборачивай стандартный вызов.
- Успех возвращается только после commit; ошибки записи, rollback и commit не скрываются. Чтение списка не создаёт N+1 и не маскирует нарушенную связь пустым объектом.
- При изменении ошибок сохраняй требуемые GraphQL extensions.code; SQL, DSN и stack trace клиенту не выдаются.

## Проверки и среда

- Выполняй gofmt, относящиеся unit-тесты, go vet и golangci-lint v2. Изменения SQL, транзакций и маппинга проверяй настоящим PostgreSQL. Сохраняй воспроизводимую генерацию gqlgen и mockgen.
- Не подавляй gocritic директивами, исключениями или отключением проверок; исправляй причину замечания.
- Интеграционные тесты и fixture лежат вместе рядом с пакетом: `internal/postgres/integration_tests/`, подготовка БД — `fixture.go`. Не оставляй тесты в корне пакета и не создавай соседний `internal/testpostgres` только ради fixture.
- Build tag — `integration`. Для PostgreSQL Dockertest запускает контейнер на пакет и отдельную БД с миграциями на тест. Docker обязателен: недоступность — ошибка, не skip. Контейнер очищает pool.Close, без AutoRemove. Config-интеграции используют MemMapFs без Docker.
- Не очищай чужие БД и Docker volumes. Проверки down/up — только в собственной тестовой БД. Моки не заменяют SQL NULL, миграции, rollback, блокировки и гонки.
- Временные файлы помещай в `.tmp` и убирай после работы. Файлы запуска и README входят в PR #1; сохраняй новые изменения пользователя при переключении веток.

<a id="go-code-style"></a>

## go-code-style

These are the user's preferences, scoped to the agreed task. Project-specific rules above take priority.

### Decomposition and existing solutions

- Do not extract a one-use helper merely to shorten a function with cyclomatic complexity at most 10. Length alone is not a reason. Reuse, a required callback or a distinct responsibility can justify extraction; state the concrete reason.
- Above 10, inspect branches and responsibilities. Do not game the metric with meaningless helpers or claim a measured value without measurement. Preserve useful helpers and resource cleanup.
- Before inventing a mechanism, inspect the standard library, existing dependencies and official online sources. Verify the selected version's actual API, compatibility and contract-relevant behavior.
- Use a suitable existing solution directly. Do not wrap it in a framework without a necessary responsibility. Custom code requires a demonstrated gap, not a guess that a library cannot do it.
- Avoid generic types, universal DTOs, error/CRUD frameworks and runners created only for similar code shapes. Small explicit repetition is acceptable. Interfaces serve actual consumers, variants or mocks, not every struct.

### Validation ownership and types

- Identify who owns presence, format, conversion and business constraints. Parsing converts representations: do not manually duplicate errors already returned for empty strings or overflow. Compare accepted values before removing checks; ParseInt accepts a plus sign.
- Validate input shape at its boundary/type; validate mutable state inside its owning operation. Revalidation needs a separate entry path or changed state, such as after locking. Do not repeat the same check across layers.
- Use `go-playground/validator/v10` and built-in range, enum and conditional rules where validation is still needed. Do not add a checks package or rename manual if chains to Validate. A needed Validate method belongs to the input owner and calls the library.
- Do not replace another project's agreed validator without a migration task. Create validator once per rule configuration and finish registration before requests.
- Use protocol/library defaults. Add dependencies for a concrete need; verify explicit zero/false/empty values are preserved. Do not write reflection/defaults machinery or apply defaults to patches.
- Domain holds business objects. Transport input belongs to transport; operation parameters belong to the operation; GraphQL codes belong to its boundary. Avoid per-layer DTO copies, import cycles and DB-to-service reverse dependencies.
- Embed shared fields; keep variant-specific fields in concrete types. Do not force meaningless zero fields on other variants or add a domain Patch[T]. Preserve absent/null/value semantics.

### Layout

- Place constructors immediately above their struct and methods below. Do not add an unnecessary constructor.
- Group adjacent ordinary declarations in `var (...)`. Inside functions, declare call results with `:=`; do not move calls into var merely to group them. Preserve initialization order/scope and use assignment for existing variables. Package scope uses var.
- Reuse existing err: `if err = operation(); err != nil`. A first `if err := ...` is allowed when err does not exist. Avoid shadowing in other nested blocks too. `rows, err := Query(...)` is appropriate in the same scope when results are needed later.
- Keep simple calls/conditions compact within the line limit. Do not split every argument unnecessarily.
- Omit explanatory comments on ordinary Go code. Preserve technical directives, build tags, licenses and generated headers. Never hand-edit generated files. Run gofmt.

### Errors and review scope

- Add context at the failure with `fmt.Errorf("operation: %w", err)`. Preserve causes; avoid universal error types/wrappers. Use sentinel errors with errors.Is when categories are needed, never text parsing.
- Translate errors into GraphQL/HTTP codes and safe client messages at transport boundaries; log internal causes on the server.
- A step fixes one agreed issue with necessary adaptations/checks. Do not combine unrelated model, validation, error and infrastructure rewrites without authorization. A request to fix PRs requires implementation, not just documenting rules.
- Test behavior, not function order or comment wording. Separate completed work from future proposals. Follow current commit/PR authorization; this section grants no independent publishing rights.

### Performance

- Evaluate request frequency, input size, traversal/reflection, allocations, locks and DB/network calls for libraries and custom code alike.
- Separate startup from per-request cost. Validator caches type/tag descriptions, not value traversal. Recursion/reflection alone does not prove a bottleneck.
- Avoid N+1, repeated whole-input validation across layers, heavy per-request initialization, needless full-domain validation and unbounded concurrency.
- Form a hypothesis before optimizing; measure CPU/heap and separately DB, pool and lock waits. Do not assert bottlenecks or present external benchmarks as project measurements.
- Compare realistic success/error cases under the same contract: ns/op, B/op, allocs/op, repeated runs and benchstat. Distinguish cold/warm behavior; measure end-to-end throughput and p95/p99 under load.
- Complex optimization needs evidence. Preserve validation, atomicity, cancellation and patch semantics. If a library blocks targets, propose a narrow measured replacement.
- Bound input, results and task counts before execution. GraphQL cost includes aliases and list size; a default complexity limit may ignore limit. Do not change agreed API limits/pagination without scope.
- Propagate one request time budget through pool waits, locks and SQL; do not restart a full timeout at each stage. Necessary cleanup may use a separate short context.
- Validate shape before transactions and state after locking. Keep begin/lock/write/commit/cleanup visible; all operation SQL uses its transaction. Canceling pgxpool.BeginTx's context does not automatically roll back.
- Size the pool using DB capacity and measured utilization/waits across all app instances. Investigate SQL and transaction duration before increasing it.
- Every goroutine needs an owner, termination condition, error delivery and completion wait. Keep work synchronous unless parallelism has concrete value; bound concurrency.
- Inspect SQL plans at representative volumes. LIMIT and absence of N+1 do not guarantee little work; large OFFSET processes skipped rows.
- Dependency changes need a reason and checks of API, transitive dependencies, Go version and affected contract. Pin generators; do not upgrade the entire tree with a local fix.

### Tests and assertions

- Actively use `github.com/stretchr/testify/require`: NoError/Error, ErrorIs/ErrorAs, Equal, Len, JSONEq, Nil/NotNil, Empty, Contains, ElementsMatch and other fitting assertions. Expected comes first in Equal; ElementsMatch only when order is irrelevant.
- No manual if+t.Fatal/t.Fatalf, reflect.DeepEqual or assertion wrappers where require already works. Resource setup helpers may use require directly. Preserve nil/empty differences, ordering and JSON meaning.
- For partial struct comparison use `require.Empty(t, cmp.Diff(want, got, cmpopts.IgnoreFields(...)))` with explicit exclusions, or compare the complete relevant section. Diff alone does not ignore fields. Config tests use NewMemMapFs; keep YAML fixtures local and indented.
- Require invokes FailNow: use it only in the test/subtest goroutine, including synchronous setup. Workers/callbacks return results; wait for them and assert in the test goroutine.
- Bind test work to t.Context(); it is canceled before t.Cleanup, so resource cleanup needs a separate bounded context. Check errors and goroutine completion.
- Table-drive cases sharing setup/action/assertions with names, inputs and expected results. Do not copy t.Run bodies; a loop of named subtests is appropriate.
- Different scenarios may stay in separate tests/subtests. Each must run independently; keep dependent scenario steps in one test. Do not turn tables into flag-heavy interpreters or arbitrary run callbacks; small mock-expectation callbacks are acceptable.
- Compare complete expected inputs/outputs when clearer than flags. Case names should explain failures. Explicitly preserve absent/null/value distinctions.
- Integration tests and fixtures belong together beside the package, as specified above; moving only the fixture is insufficient.

### Mocks and generation

- Never add functions, exports, constructors, flags or hooks to production/business packages solely for tests, even behind an integration build tag. Keep test support in test files or dedicated integration fixtures; use real public operations and external observation. Do not expose private state to satisfy a test.
- Mock suitable boundaries to test interactions, arguments and errors. A mock returns configured results; it must not implement the real service's validation. A mocked error proves caller handling, not the real dependency's check.
- Interfaces belong to actual consumers; simple values need no mocks. Real PostgreSQL integration coverage remains mandatory for migrations, NULL, transactions, locks and races.
- Put a working `//go:generate` directly above the interface being mocked. Mockgen implements interfaces; do not add fake directives to every struct.
- Use `go.uber.org/mock/mockgen`. Pin its version and specify source/type, output and package; generation runs from the package directory. Generated files are not hand-edited and must reproduce exactly. Go test does not run go generate.
- Give each independent subtest its own mock/controller. Check necessary interactions; avoid blanket AnyTimes and ordering requirements absent from the contract. Gomock.NewController(t) already registers cleanup; finish workers before it.

### Suggestions, not additional mandates

- A boolean selecting a business scenario may hide different errors; a boolean representing data/presence is different.
- Expand dense struct literals one field per line when useful.
- Use t.Helper/t.Cleanup for shared resource setup; keep main assertions visible in the test.
- Use t.Parallel only with independent resources. Synchronize concurrency tests with bounded events, not random Sleep.
- Add assertion context where needed. Use ErrorIs/ErrorAs for categories; exact error text only if contractual.
- Normally pass replaceable dependencies explicitly instead of mutating package globals for tests; the project's global logger is an agreed exception. Export only necessary APIs and prefer responsibility-based names over util/common/helpers.

Sources: [Go testing](https://pkg.go.dev/testing), [validator](https://github.com/go-playground/validator), [mockgen](https://github.com/uber-go/mock).

<a id="pr-workflow"></a>

## pr-workflow

- Сначала проверь текущую ветку/дерево и отдели свои изменения от пользовательских. Соблюдай проектные имена веток и коммитов; если разрешена hotfix-ветка без номера задачи, не выдумывай номер.
- До commit/push/merge/squash/rebase и удалённых изменений проверь уже выданное разрешение. В рамках задачи PR #1 commit/push разрешены; main сливает пользователь. Запрашивай только недостающую авторизацию.
- Перед новым PR/MR определи заголовок, описание, базу, draft, squash и удаление исходной ветки с учётом договорённостей. Самостоятельно не создавай новые PR поверх текущего общего PR #1.
- Описание содержит конкретную проблему, итоговое поведение и нужные технические/проверочные детали, без маркетинга и истории обсуждения. Если workflow требует ревью перед push, сначала покажи изменения по файлам и проверки; текущий проект использует ревью через PR.
- Сначала собери замечания; различай нужные правки, вопросы и не требующие действия комментарии. Исправляй согласованное, не отмечай обсуждение решённым до исправления. В отчёте укажи закрытые замечания и оставшиеся вопросы.
- Для CI проверяй последний pipeline/check текущей ветки/PR; читай логи упавших jobs, ищи причину, не только симптом. После исправления запускай относящиеся локальные проверки. Удалённые retry/cancel требуют разрешения пользователя.
- Итог: что и где изменено, проверено/не проверено, ограничения и только действительно ещё требующие решения действия. Для GitLab CLI дополнительно действует следующий раздел glab.

<a id="semver"></a>

## semver

Применяется при выборе/проверке версий релизов, тегов, changelog, пакетов, API, сервисов, образов, Helm charts и политики миграций. Источник версии и политика проекта имеют приоритет.

Формат: `MAJOR.MINOR.PATCH[-PRERELEASE][+BUILD]`, например `1.4.2`, `2.0.0-rc.1+build.45`.

- **MAJOR** — несовместимость публичного API, CLI, схемы данных, wire format, миграций или другого контракта потребителей: удалённое поле/метод, изменённая семантика, необходимость менять клиента или ручной несовместимый шаг. Пример: `1.8.4 → 2.0.0`.
- **MINOR** — совместимая новая возможность: поле, команда, необязательный параметр, настройка с совместимым default, расширение поведения с сохранением старых сценариев. Пример: `1.8.4 → 1.9.0`.
- **PATCH** — совместимое исправление/внутреннее улучшение: баг, гонка, производительность без изменения поведения, безопасность без несовместимости; документация, если проект версионирует такие правки. Пример: `1.8.4 → 1.8.5`.
- При MAJOR обычно обнуляются MINOR/PATCH, при MINOR обнуляется PATCH.
- Pre-release отделяется `-`: `1.9.0-alpha.1`, `-beta.2`, `-rc.1`; он ниже финальной версии с теми же числами: `1.9.0-rc.1 < 1.9.0`.
- Метаданные отделяются `+`: `1.9.0+build.45`, `1.9.0+sha.abc123`; они не влияют на порядок версий.
- Сначала найди текущую версию в теге/changelog/manifest/release/config и перечень изменений; установи наличие breaking change, затем выбери MAJOR/MINOR/PATCH. Для нефинального релиза добавь suffix, CI/commit metadata — только если политика разрешает.
- Не угадывай bump при неизвестных текущей версии, изменениях или совместимости. Объясни предложенный переход через конкретное изменение контракта. Выбор версии сам по себе не разрешает публикацию релиза.

<a id="glab"></a>

## glab

Правила применяются только при работе с GitLab через glab: auth, репозиторий, MR, pipeline/jobs, issues, labels, releases. Этот GitHub-проект не требуется переносить в GitLab.

Перед **каждым** запуском очищай `HTTP_PROXY`, `HTTPS_PROXY`, `http_proxy`, `https_proxy` в том же окружении. Исключение — явная просьба пользователя проверить proxy-сценарий.

```powershell
$env:HTTP_PROXY=""
$env:HTTPS_PROXY=""
$env:http_proxy=""
$env:https_proxy=""
glab <command>
```

- Сначала `git status --short --branch`; при доступе к GitLab — `glab auth status`.
- Не угадывай флаги. Для незнакомого — `glab --help`, `glab <command> --help`, `glab <command> <subcommand> --help`; фактическая версия/справка важнее памяти и примеров.
- Чтение: `glab repo view`, `mr list`, `mr view <iid>`, `pipeline list`, `pipeline view <id>`, `job view <id>`, `issue view <iid>`.
- MR: `glab mr list --source-branch <branch>`; до создания/изменения изучи `glab mr create --help` / `glab mr update --help`.
- CI: `glab ci status`, `glab pipeline list --per-page 10`; retry/cancel — только после проверки справки и разрешения пользователя.
- Issues/labels/releases: `glab issue list`, `glab label list`, `glab release list`, `glab release view <tag>`.
- Create/update/note/merge/delete/release и изменения issues/labels меняют удалённое состояние. Сначала проверь текущие разрешения пользователя/проекта; спрашивай только если действия ещё не разрешены. Правила PR #1 остаются в силе.
- При сбое сохрани точные stdout/stderr, затем проверь help до повторения. Диагностика: `glab --version`, auth status, help, `git remote -v`, очистка proxy-переменных в том же запуске.
