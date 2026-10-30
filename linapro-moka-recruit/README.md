# linapro-moka-recruit

Go client for the Moka recruiting OpenAPI, shared by the source plugins that
talk to Moka recruiting. It is a **library**, not a plugin: it has no
`plugin.yaml`, no host registration, no tables and no jobs, and it is consumed
through a `go.mod` `require` + `replace` directive.

The module name intentionally carries no `lina-plugin-` prefix. Repository
governance treats that prefix as a plugin owner marker and would report every
importer as crossing a plugin boundary.

## Authentication

Both modes implement the `Author` interface and are injected into `NewClient`.

| Mode | Constructor | Behavior |
|------|-------------|----------|
| `BasicAuth` | `NewBasicAuth(BasicCredential{APIKey})` | `Authorization: Basic base64(apiKey:)`, the main mode for recruiting |
| `OAuth2Auth` | `NewOAuth2Auth(baseURL, OAuth2Credential{ClientID, ClientSecret})` | `client_credentials` Bearer token, renewed automatically |

## API path prefix

All endpoints live under `/api-platform/` on `DefaultBaseURL`
(`https://api.mokahr.com`). The wrapped endpoints currently use the
`/api-platform/v1/`, `/api-platform/v2/`, `/api-platform/candidate/v1/` and
`/api-platform/application/` prefixes. Any other recruiting endpoint can be
called through `PostJSON`, `GetJSON` or `PutQuery` with its full path.

## What it provides

- **Transport** (`client.go`) — `NewClient(baseURL, auth)`, `PostJSON`,
  `GetJSON`, `PutQuery`.
- **Applications** (`application.go`) — `EhrApplications`,
  `MoveApplicationStage`, `GetInterviewInformation`, `GetHandleResult`.
- **Resumes** (`resume.go`) — `GetResumeContent`.
- **Stages** (`stage.go`) — `GetStagesList`.
- **Reports** (`report.go`) — `GetReportData` with `ReportHeader` / `ReportData`.

## Usage

```go
import "linapro-moka-recruit/moka"

auth := moka.NewBasicAuth(moka.BasicCredential{APIKey: apiKey}) // config value
client := moka.NewClient(baseURL, auth) // empty baseURL falls back to DefaultBaseURL
report, err := client.GetReportData(ctx, reportID)
```

The consumer `go.mod` needs:

```text
require linapro-moka-recruit v0.0.0
replace linapro-moka-recruit => ../linapro-moka-recruit
```

Credentials are read from the consuming plugin's own runtime configuration and
are never stored in this library or in the database.

## Dependents

`linapro-moka-report-sync` (report to Bitable sync) and
`linapro-recruit-pipeline` (recruiting pipeline orchestration).
