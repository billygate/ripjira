# ripjira — Design Spec

**Дата:** 2026-04-30
**Автор:** A. Mikhaylov
**Статус:** approved (brainstorm)

## 1. Обзор

`ripjira` — консольная TUI-утилита, Jira CLI-клиент для повседневной работы:
посмотреть свои задачи, открыть детали, выполнить базовые действия (создать,
сменить статус, назначить, прокомментировать). Полноэкранный двухпанельный
интерфейс в стиле `lazygit`/`k9s`, с переключаемыми темами в духе Neovim.

### Goals

- Быстрый запуск: показать "мои задачи" мгновенно из дискового кэша, обновить в фоне.
- Полностью неблокирующий UI: все сетевые вызовы — асинхронные, с возможностью отмены.
- Эстетика: пакеты тем (Tokyo Night, Catppuccin, Gruvbox, Nord, Rose Pine).
- Минимальный, но достаточный набор действий для ежедневного флоу разработчика.

### Non-goals (на MVP)

- Доска спринта / Kanban-вью.
- Произвольный JQL-поиск, бэклог, эпики, advanced roadmaps.
- Worklog (логирование времени).
- Markdown-рендер описаний/комментариев — только plain text.
- Поддержка Jira Server / Data Center — только Cloud.

## 2. Стек и зависимости

- **Язык:** Go.
- **TUI:** `github.com/charmbracelet/bubbletea`, `bubbles` (`list`, `textarea`,
  `viewport`, `spinner`, `textinput`), `lipgloss`.
- **Конфиг:** `gopkg.in/yaml.v3`.
- **Секреты:** `github.com/zalando/go-keyring` (macOS Keychain).
- **Jira API:** собственный HTTP-клиент поверх `net/http` (REST API v3, Cloud).
  Сторонние SDK не используем — они тяжёлые и неполные.

## 3. Архитектура

### Дерево пакетов

```
ripjira/
├── cmd/ripjira/main.go         # точка входа
├── internal/
│   ├── config/                 # YAML-конфиг, валидация, first-run wizard
│   │   └── keyring.go          # интерфейс SecretStore + go-keyring impl + fake
│   ├── jira/                   # HTTP-клиент Jira Cloud REST v3
│   │   ├── client.go           # base client, auth, retries, контекст
│   │   ├── issues.go           # myIssues, getIssue, transitions, assign
│   │   ├── comments.go         # addComment + text → ADF конвертер
│   │   ├── create.go           # createmeta + createIssue
│   │   └── types.go            # доменные структуры (Issue, Comment, …)
│   └── tui/
│       ├── app.go              # корневая Bubble Tea модель, роутинг сообщений
│       ├── styles/             # Lip Gloss стили, привязанные к Palette
│       ├── themes/
│       │   ├── theme.go        # интерфейс Palette + Style getters
│       │   ├── tokyonight.go
│       │   ├── catppuccin.go
│       │   ├── gruvbox.go
│       │   ├── nord.go
│       │   └── rosepine.go
│       ├── panes/
│       │   ├── list.go         # левая панель: issues с группировкой
│       │   └── detail.go       # правая панель: описание + комменты
│       ├── overlays/
│       │   ├── create.go       # динамическая форма по createmeta
│       │   ├── transition.go   # выбор статуса
│       │   ├── assign.go       # user picker
│       │   ├── comment.go      # textarea для комментария
│       │   └── help.go         # ?-cheatsheet
│       └── grouping.go         # стратегии (Status, Priority, …)
└── go.mod
```

### Поток данных

UI → `tea.Cmd` (async) → `jira.Client` → Jira REST API
UI ← `tea.Msg` (loaded / error) ← Jira REST API

UI-loop никогда не блокируется. Все действия с сетью — через `tea.Cmd`,
ответы прилетают как сообщения в `Update`.

### Принципы изоляции

- Доменные структуры (`Issue`, `Comment`, `User`) принадлежат `internal/jira`,
  UI-слой не знает о форме API. Маппинг DTO → domain — в `client.go`.
- Стили UI обращаются только к именованным цветам палитры темы — никаких hex
  в коде вьюх. Это даёт честное переключение тем и читаемый стилевой слой.
- `internal/config` ничего не знает о UI и о Jira-клиенте; оба зависят от него.
- Keychain прячется за интерфейсом `SecretStore`, чтобы тесты не дёргали
  системный keychain.

## 4. UI и UX

### Главный экран — two-pane

```
┌─ ripjira ───────────────────────────── tokyonight ─ ⟳ refreshing… ─┐
│  Group: status   ▼          │  PROJ-142  ● High                    │
│                             │  Fix login redirect on Safari        │
│  ▸ In Progress  (2)         │  ─────────────────────────────────── │
│    PROJ-142  ● Fix login…   │  Status: In Progress                 │
│    PROJ-118  ◐ Refactor a…  │  Assignee: you                       │
│  ▸ To Do  (5)               │  Updated: 2h ago                     │
│    PROJ-201  ◌ Add metrics  │  ── Description ──────────────────── │
│    PROJ-199  ● Migrate db…  │  When user logs in via Safari 17,    │
│  ▸ In Review  (1)           │  the redirect to /dashboard fails…   │
│                             │  ── Comments (3) ─────────────────── │
├────────────────────────────────────────────────────────────────────┤
│ ↑↓ nav  →/Tab focus  enter open  s status  c comment  n new  ? help│
└────────────────────────────────────────────────────────────────────┘
```

- **Левая панель** — `bubbles/list` с кастомным рендером строк (ID, иконка
  приоритета, обрезанный summary). Группы сворачиваемые по `space`.
- **Правая панель** — `bubbles/viewport` со скроллом; секции *Description*,
  *Comments*, *Activity*.
- **Топ-бар** — текущая группировка, имя темы, индикатор фоновой загрузки.
- **Низ** — контекстный hint-bar (меняется по фокусу/режиму).

### Оверлеи (модалки)

Полупрозрачный backdrop + центрированный бокс. Закрытие — `esc` везде.
Используются для: создание задачи, выбор transition, user picker для assign,
ввод комментария, help.

### Иконки и цвета

- Приоритет: `🔥` Highest / `●` High / `◐` Medium / `◌` Low / `·` Lowest
  (Unicode-символы; для `icons: ascii` — буквенные алиасы).
- Статус — окрашивается через семантику палитры (`status.todo`,
  `status.inProgress`, `status.done`, `status.blocked`).
- Тип задачи — мини-бейдж `[B]` Bug / `[T]` Task / `[S]` Story / `[E]` Epic.

### Группировка

Переключаемая стратегия (`internal/tui/grouping.go`). Для MVP:
- `1` — по статусу (дефолт).
- `2` — по приоритету.
Дизайн расширяемый: новая группировка = новая реализация интерфейса
`Strategy { Group(issues) []Group }`.

`default_grouping` из конфига задаёт начальное состояние при запуске приложения.
Переключения `1`/`2` во время сессии — только в памяти, в конфиг не пишутся.

### Epic links

Issues carry an optional `ParentKey` / `ParentSummary` populated from
`fields.parent` on every fetch. The detail pane renders an `Epic` row
above Labels. `E` opens the epic picker (single-column list with
type-to-filter, leading "⊘ No epic (detach)" row when current parent
is set). The picker dispatches an optimistic `SetParent` PUT;
failures revert the local state and surface a toast. The `parent`
grouping strategy buckets tasks under their parent — epics on top,
then one bucket per parent, then a trailing "No epic" bucket.
`epic_issue_types` in `config.yaml` controls which issuetype names
count as epic-shaped (defaults to `[Epic, "Epic Feature"]`).

### Tabs (two-level)

The tab strip is two rows. **Top row** groups views by scope:
`MY ISSUES` (personal: assigned/watching/reported/recent/mentions),
`SPRINT`, `STRUCTURES`, `SEARCH` — the latter three are project-scoped.
**Sub row** appears only when the active top has more than one sub-view; it
lists the scope inside the group (`ASSIGNED`, `WATCHING`, …).
Navigation: `}`/`{` cycles the top row; `]`/`[` cycles the sub row. The last
sub-view per top is persisted in `state.LastSubView` so re-entering a top
returns to the user's previous scope.

### Structures

The `STRUCTURES` tab swaps the flat grouping for **named sections** defined
per project. Built-ins (`default`, `inbox`) ship in code; user structures
live as YAML at `~/.config/ripjira/structures/<PROJECT>.yml` and hot-reload
through `fsnotify`. JSON tags on `internal/structure` types match pilot's
DTOs byte-for-byte so external sync can drop pilot's REST output as a file.
The tab uses `\` to open the picker overlay; `}` / `{` cycles structures.
Selection persists per-project via `state.LastStructure`.

### Темы

Пакет `internal/tui/themes` определяет интерфейс `Palette` с именованными
цветами (`bg`, `fg`, `accent`, `muted`, `red`, `green`, `yellow`, `blue`,
`magenta`, `cyan`) и семантикой (`priority.high`, `status.inProgress`, …).
Все стили UI ссылаются на палитру. Lip Gloss автоматически деградирует до 256
цветов, если терминал не поддерживает truecolor.

Переключение темы — через `theme: <name>` в `config.yaml`, требует рестарта.

### Keymap

| Клавиша | Действие |
|---|---|
| `↑/↓` или `j/k` | Навигация по списку |
| `→/Tab` / `←/S-Tab` | Переключение фокуса между панелями |
| `g/G` | В начало / в конец списка |
| `space` | Свернуть/развернуть группу |
| `1` / `2` | Группировка: `1` status, `2` priority |
| `enter` | Развернуть задачу на весь экран |
| `s` | Сменить статус |
| `a` | Назначить |
| `c` | Добавить комментарий |
| `n` | Новая задача |
| `o` | Открыть в браузере |
| `r` | Принудительный refresh |
| `/` | Локальный фильтр по списку |
| `?` | Help-cheatsheet |
| `esc` | Закрыть оверлей / снять фокус |
| `q` или `Ctrl+C` | Выйти |

Help-cheatsheet генерируется из единого реестра keybindings, чтобы help не
расходился с реальным поведением.

## 5. Jira-интеграция

### HTTP-клиент

```go
type Client struct {
    baseURL    *url.URL    // https://<tenant>.atlassian.net
    email      string
    token      string      // достаём из keychain
    http       *http.Client
    accountID  string      // кэшируем после первого /myself
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error
```

- **Auth:** `Authorization: Basic base64(email:token)`.
- **Retries:** 2 попытки на 5xx и сетевые ошибки, exponential backoff.
- **Rate limit:** уважаем `Retry-After` из 429.
- **Контекст:** все методы принимают `context.Context` — для отмены при быстром
  перелистывании.
- **Логирование:** debug-лог в `~/.cache/ripjira/debug.log` при `RIPJIRA_DEBUG=1`,
  заголовок `Authorization` маскируется.

### Эндпоинты MVP (REST API v3)

| Метод клиента | Endpoint |
|---|---|
| `MyIssues(ctx)` | `GET /rest/api/3/search/jql` с JQL `assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC` |
| `GetIssue(ctx, key)` | `GET /rest/api/3/issue/{key}?fields=*all&expand=renderedFields` |
| `GetTransitions(ctx, key)` | `GET /rest/api/3/issue/{key}/transitions` |
| `DoTransition(ctx, key, id)` | `POST /rest/api/3/issue/{key}/transitions` |
| `AssignIssue(ctx, key, accountID)` | `PUT /rest/api/3/issue/{key}/assignee` |
| `AddComment(ctx, key, body)` | `POST /rest/api/3/issue/{key}/comment` (ADF) |
| `SearchUsers(ctx, query)` | `GET /rest/api/3/user/search?query=…` |
| `Myself(ctx)` | `GET /rest/api/3/myself` |
| `CreateMeta(ctx, project, type)` | `GET /rest/api/3/issue/createmeta/{key}/issuetypes/{typeId}` |
| `CreateIssue(ctx, payload)` | `POST /rest/api/3/issue` |

### ADF (Atlassian Document Format)

Cloud принимает description/comments только как ADF-дерево. Для MVP — минимальный
конвертер `text → ADF`: параграфы по `\n\n`, переносы по `\n` как `hardBreak`.
При чтении — берём `renderedFields` (HTML) и упрощаем до plain через простой
stripper тегов. Никакого markdown в MVP.

### Динамическая форма создания

Поток UX:

1. `n` — открывается оверлей.
2. **Шаг 1**: выбор проекта. Список из `/project/search`, дефолт — `default_project`
   из конфига. Фильтрация по вводу.
3. **Шаг 2**: выбор issue type — из `meta.projects[i].issuetypes`. Дефолт — Task.
4. **Шаг 3**: запрашиваем `createmeta` для пары `project+issuetype`. Получаем поля
   с метаданными (`required`, `schema.type`, `allowedValues`, `name`).
5. **Шаг 4**: динамически рендерим форму. Маппинг типов → виджеты:

| Jira schema | Виджет |
|---|---|
| `string` (summary) | `textinput` |
| `string` с ADF (description) | `textarea` |
| `option` / `priority` / `issuetype` | select из `allowedValues` |
| `user` | user picker (поиск с дебаунсом) |
| `array<option>` | мульти-select / chips |
| `number` | `textinput` с валидацией |
| `date` / `datetime` | `textinput` с маской `YYYY-MM-DD` |
| неизвестный тип | пропускаем + предупреждение в footer |

6. **Submit** (`Ctrl+S`): собираем payload, конвертируем description в ADF, шлём
   `POST /issue`. На успех — оверлей закрывается, новая задача добавляется в список
   мгновенно, snackbar `Created PROJ-203`. По `o` из snackbar — открыть в браузере.

**Required-валидация** на клиенте: пустые required-поля подсвечиваются, submit
блокируется. Серверные ошибки (`400` с `errors`) парсим и показываем у конкретного
поля.

**Кэш `createmeta`** — на сессию в RAM, ключ `project+type`.

### Доменная модель (выжимка)

```go
type Issue struct {
    Key         string
    Summary     string
    Status      Status
    Priority    Priority
    Type        IssueType
    Assignee    *User
    Reporter    *User
    Description string         // plain text из renderedFields
    Comments    []Comment
    Updated     time.Time
    Transitions []Transition
    URL         string         // baseURL + "/browse/" + key
}
```

## 6. Конфиг и секреты

### Файлы

```
~/.config/ripjira/config.yaml      # настройки (XDG)
~/.cache/ripjira/issues.json       # кэш списка
~/.cache/ripjira/debug.log         # debug-лог при RIPJIRA_DEBUG=1
```

API-токен **не хранится в файлах** — только в macOS Keychain через `go-keyring`,
service: `ripjira`, account: `email`.

### `config.yaml`

```yaml
# обязательное
base_url: https://acme.atlassian.net
email: you@acme.com

# поведение
default_project: PROJ          # для формы создания, опционально
default_grouping: status       # status | priority
auto_refresh_seconds: 0        # 0 = off; 60 — пуллить раз в минуту

# внешний вид
theme: tokyonight              # tokyonight | catppuccin | gruvbox | nord | rosepine
icons: unicode                 # unicode | ascii
```

Минимально валидный конфиг — `base_url` + `email`.

### First-run wizard

Запускается, если конфига нет или не хватает обязательных полей. В стиле тех
же оверлеев Bubble Tea:

1. **Jira URL** (`textinput`, валидация формата URL).
2. **Email** (`textinput`, валидация формата).
3. **API token** (password-режим). Под полем — серая ссылка на
   `https://id.atlassian.com/manage-profile/security/api-tokens`.
4. **Проверка**: спиннер → `GET /myself`. На успех — `Connected as <displayName>`,
   на ошибке — текст ошибки и возврат на шаг 3.
5. **Default project** (опционально): селект из `/project/search` с фильтром.
   Esc — пропустить (форма создания будет каждый раз спрашивать проект).
6. **Сохранение:** `config.yaml` с правами `0600`, токен — в Keychain.

Перевыпуск — `ripjira login` или `ripjira login --reset` (затирает Keychain).

### Безопасность

- Никогда не пишем токен в файлы или логи.
- Проверяем права `config.yaml`: если шире `0600` — предупреждение в footer.
- Если Keychain недоступен — fallback: `RIPJIRA_TOKEN` env var, никуда не пишем.

### CLI

```
ripjira              # запуск TUI (основной use case)
ripjira login        # перевыпуск настроек/токена
ripjira login --reset
ripjira --version
ripjira --help
```

## 7. Ошибки

Три уровня — пользователь видит только то, что может починить.

**1. Восстановимые (toast снизу, 4 сек):**
- Сетевая ошибка — клиент уже сам ретраит, toast информирующий.
- Rate limit 429 — toast с обратным отсчётом до `Retry-After`.
- Конфликт оптимистичного действия (статус сменился у другого) — toast
  `Outdated, refreshing…` + автоматический рефреш задачи.

**2. Действие провалилось (inline в оверлее):**
- 400 от Jira при создании/комментарии — парсим `errors`, показываем у поля.
- 403 при transition — баннер `Permission denied` сверху оверлея.

**3. Фатальные (full-screen overlay):**
- 401 — токен невалиден → инструкция `ripjira login --reset` + быстрый перезапуск
  визарда по `r`.
- Конфиг повреждён — путь к файлу, причина парсинга, выход.
- Нет сети при старте и кэш пустой — сообщение, `r` для retry.

Все ошибки логируются в `~/.cache/ripjira/debug.log` с маскированной авторизацией
при `RIPJIRA_DEBUG=1`.

## 8. Перформанс и асинхронность

| Сценарий | Поведение |
|---|---|
| Холодный старт | Если кэш есть → мгновенный рендер списка из кэша + спиннер `refreshing` → фоновый запрос → diff и обновление. Если кэша нет → пустой список + спиннер. |
| Открытие задачи | Сразу показываем то, что уже есть в списке (summary, status, assignee). Параллельно через `errgroup` грузим details + comments + transitions. Skeleton-плейсхолдеры заменяются по приходу. |
| Перелистывание ↓↓↓ | На каждый шаг — новый `context.Context` для деталей, предыдущий отменяется. Goroutines выходят по `ctx.Done()`. |
| Действие | Оптимистично: UI обновляется немедленно, payload — в фоне. На ошибке — откат + toast. |
| Auto-refresh | При `auto_refresh_seconds > 0` — `time.Ticker`, тихий рефреш списка. Не дёргает деталь открытой задачи. |
| Manual refresh `r` | Жёсткий рефреш списка и текущей задачи. |
| Локальный фильтр `/` | По уже загруженному списку, без сети. Дебаунс 80мс. |
| User-picker | `/user/search` с дебаунсом 250мс, минимум 2 символа. Кэш результатов на сессию по подстроке. |

## 9. Кэш на диске

Файл: `~/.cache/ripjira/issues.json`

```json
{
  "version": 1,
  "fetched_at": "2026-04-30T17:42:11Z",
  "account_id": "5b10ac8d82e05b22cc7d4ef5",
  "issues": [
    /* минимальные поля для списка: key, summary, status, priority, type,
       assignee, updated */
  ]
}
```

- Запись — атомарная: `issues.json.tmp` → `rename`.
- Привязан к `account_id` — при смене аккаунта инвалидируется.
- Никаких комментов/описаний на диске — только список. Открыли задачу →
  details идут в RAM, не на диск.
- TTL не нужен: при старте всегда триггерим фоновый рефреш, кэш только для
  мгновенной отрисовки.

## 10. Тестирование

**Unit-тесты `internal/jira/` (главный фокус):**
- HTTP-клиент через `httptest.Server` — корректность URL, методов, заголовков,
  парсинг ответов, обработка 4xx/5xx/429.
- ADF-конвертер — табличные тесты на edge cases.
- DTO → domain маппинг — табличные тесты с фикстурами в `testdata/`.
- Покрытие — целимся ≥80% для пакета `jira`.

**Unit-тесты `internal/config/`:**
- Загрузка/сохранение YAML, валидация обязательных полей.
- Keychain — через интерфейс `SecretStore` с fake.

**TUI-тесты:**
- `github.com/charmbracelet/x/exp/teatest`: отправляем key-events, проверяем
  вывод по golden-файлам.
- Ключевые сценарии: навигация, открытие деталей, переключение группировки,
  открытие/закрытие оверлеев.
- Темы — только дефолтная в TUI-тестах; палитры тестируются отдельным unit-ом.

**Что НЕ тестируем:**
- Реальные походы в Jira (никаких e2e против живого инстанса).
- Точный пиксельный рендер всех экранов.

**Tooling:**
- `go test ./...`
- `golangci-lint` (govet, staticcheck, errcheck, revive)
- Makefile: `test`, `lint`, `build`, `run`

## 11. Порядок реализации

Каждый шаг — отдельный коммит с зелёными тестами. Цель — рабочий артефакт как
можно раньше, потом наращивание.

### Этап 1 — Скелет и Jira-клиент

1. `go mod init`, базовая структура каталогов, Makefile, линтер.
2. `internal/config/`: чтение YAML, тесты.
3. `internal/config/keyring.go`: интерфейс `SecretStore` + go-keyring impl + fake.
4. `internal/jira/client.go`: HTTP-клиент с auth, retries, контекстом.
5. `internal/jira/issues.go`: `Myself`, `MyIssues`, `GetIssue`, `GetTransitions`,
   `DoTransition`, `AssignIssue`. Тесты через `httptest`.
6. `internal/jira/comments.go`: `AddComment` + ADF-конвертер. Тесты.
7. `internal/jira/create.go`: `CreateMeta`, `CreateIssue`. Тесты.
8. `cmd/ripjira/main.go`: точка входа, печатает `MyIssues` в stdout — sanity-чек.

### Этап 2 — TUI: чтение

9. `internal/tui/themes/`: интерфейс + Tokyo Night.
10. `internal/tui/styles/`: стили, привязанные к теме.
11. `internal/tui/app.go`: корневая модель, layout двух панелей, базовые сообщения.
12. `internal/tui/panes/list.go`: рендер списка с группировкой по статусу, навигация.
13. `internal/tui/grouping.go`: стратегии Status/Priority, переключение `1`/`2`.
14. `internal/tui/panes/detail.go`: правая панель, async-загрузка деталей и комментов.
15. Кэш на диске — мгновенный старт.
16. Toast-bar и spinner-индикатор.
17. `?` — help-оверлей с реестром keybindings.

### Этап 3 — TUI: действия

18. `overlays/transition.go` — смена статуса, оптимистично.
19. `overlays/comment.go` — добавление комментария.
20. `overlays/assign.go` — user picker с дебаунсом.
21. `o` — открыть в браузере.
22. `r` — manual refresh; `auto_refresh_seconds` через ticker.

### Этап 4 — Создание задачи

23. `overlays/create.go` — пошаговая форма (project → type → dynamic fields).
24. Динамический рендер полей по schema-типам.
25. Кэш `createmeta` на сессию.
26. Required-валидация + парсинг серверных ошибок.

### Этап 5 — Полировка

27. Остальные темы (Catppuccin, Gruvbox, Nord, Rose Pine).
28. First-run wizard (до этого — ручное создание `config.yaml`).
29. `ripjira login` / `ripjira login --reset`.
30. README и скриншоты.

После этапа 2 у пользователя уже работающая read-only утилита. После этапа 3 —
полностью функциональная для повседневной работы. Этап 4 закрывает создание.
Этап 5 — UX-полировка.
