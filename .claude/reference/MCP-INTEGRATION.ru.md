# raxd — интеграция MCP (источник истины)

> Контракт для `mcp-engineer` (проектирует) и `developer` (реализует). `reviewer` проверяет
> соответствие. Не путать роли: mcp-engineer пишет `mcp-spec.md`, код пишет developer.

## Что такое MCP здесь

Model Context Protocol — стандарт, по которому ИИ-агент (клиент) обращается к серверу за
инструментами и данными. Версия спецификации — ориентир **2025-11-25**. Основные понятия:
- **Tools** — действия, которые агент может вызвать (у нас: выполнить команду, загрузить файл).
- **Resources** — данные/контекст для чтения (у нас: статус демона, список команд/возможностей).
- **Prompts** — шаблоны (опционально, при необходимости).

## SDK

Официальный Go SDK: `github.com/modelcontextprotocol/go-sdk/mcp` (предпочесть community
`mark3labs/mcp-go`). Базовый каркас:

```go
import "github.com/modelcontextprotocol/go-sdk/mcp"

server := mcp.NewServer(&mcp.Implementation{Name: "raxd", Version: version}, nil)

type ExecInput struct {
    Command string   `json:"command"`
    Args    []string `json:"args,omitempty"`
}
func execHandler(ctx context.Context, req *mcp.CallToolRequest, in ExecInput) (
    *mcp.CallToolResult, any, error) { /* exec.Command, таймаут, аудит */ }

mcp.AddTool(server, &mcp.Tool{Name: "exec", Description: "Run a command on the host"}, execHandler)
```

## Транспорт (важно)

`raxd` обслуживает удалённых сетевых клиентов → **stdio не подходит**. Используем
**Streamable HTTP поверх TLS**:
- единый эндпоинт, например `https://<host>:<port>/mcp`;
- POST — запросы клиента (JSON-RPC); GET — SSE-поток сервер→клиент;
- stateless-дружелюбно (сессии через `Mcp-Session-Id`);
- ОБЯЗАТЕЛЬНО: TLS (см. SECURITY-BASELINE), валидация `Origin`, аутентификация по API-ключу
  (заголовок, напр. `Authorization: Bearer rax_live_…`) ДО исполнения любого tool.

## Предлагаемый набор tools/resources (первая итерация)

- tool `exec` — выполнить команду (вход: command, args, опц. timeout; выход: stdout/stderr/exit).
- tool `upload_file` — загрузить файл (вход: path, content/base64 или поток; выход: статус, размер).
- resource `status` — состояние демона/версия/аптайм.
- resource `capabilities` — список доступных команд/ограничений (в т.ч. активен ли allowlist).

Каждый вызов tool проходит: аутентификация → rate-limit → аудит-лог → исполнение → аудит результата.

## Где брать детали

Стек и пути — `STACK.ru.md`. Правила безопасности (ключи, TLS, exec, аудит) — `SECURITY-BASELINE.ru.md`.
Актуальность спецификации/SDK при сомнениях уточняет `research-analyst` через WebFetch.
