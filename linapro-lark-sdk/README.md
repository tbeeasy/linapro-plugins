# linapro-lark-sdk

Shared Go library for Lark / Feishu outbound access, consumed by first-party
plugins (recruit-pipeline, moka-report-sync, eval-360) via a go.mod `replace`
directive. It is **not** a plugin — it has no `plugin.yaml`, registers no host
hooks, and ships no SQL or menus.

## What it owns

- **`larkhttp`** — a custom `HttpClient` wrapping the official Lark SDK v3 that
  uniformly handles **HTTP 429 rate-limit backoff**. The SDK's built-in retry
  only covers dial failures and token expiry; this package reads
  `x-ogw-ratelimit-reset` (falling back to `Retry-After`, then to jittered
  exponential backoff) and retries. Because the SDK routes token requests
  through the same `HttpClient`, token refreshes throttled by 429 also
  back off automatically.
- **`larkbitable`** — bitable (多维表格) record read/write: batch
  `list`/`get`/`create`/`update`/`batch_create`/`batch_update` with paging,
  field-type-aware value encoding, and a `Persons` bypass so callers can hand
  pre-resolved `open_id` sets straight to the SDK for serialization.
- **`larkcontact`** — contact (通讯录) enumeration over both the own tenant
  and associated organizations, returning users tagged with `own`/`assoc`
  `Source` and deduplicated by `open_id`.
- **`larkim`** — IM outbound messaging: send interactive cards
  (`msg_type=interactive`) to a receiver (`open_id` / `user_id` / `email` /
  `chat_id`), reusing `larkhttp`'s 429 backoff. Token management is left to the
  official SDK. Also exposes `BuildActionCard` to assemble a minimal
  title + markdown + action-button card JSON.
- **`utils`** — small formatting helpers shared across the sub-packages.

## Boundary constraints

- Only **outbound** calls. The library MUST NOT implement event subscription or
  callback reception.
- Dependency direction is intra-library (`larkim` → `larkhttp`,
  `larkbitable` → `larkhttp`). The library MUST NOT import `lina-core`.
  (`larkhttp` currently imports `lina-core/pkg/logger` for 429 backoff logging
  at the source layer, resolved via `go.work`; the other sub-packages add no
  `lina-core` references.)

## Consumers

| Consumer plugin | Uses |
|-----------------|------|
| `linapro-recruit-pipeline` | `larkbitable` (write verdicts / persons), `larkhttp` |
| `linapro-moka-report-sync` | `larkbitable` (write report persons), `larkhttp` |
| `linapro-eval-360` | `larkim` (one-card-per-evaluator push) |

See `openspec/changes/archive/2026-09-20-extract-shared-lark-sdk/` for the
extraction design.
