# linapro-moka-hcm

Minimal Go client for the Moka HCM OpenAPI, shared by the source plugins that
talk to Moka. It is a **library**, not a plugin: it has no `plugin.yaml`, no
host registration, no tables and no jobs, and it is consumed through a go.mod
`require` + `replace` directive.

The module name intentionally carries no `lina-plugin-` prefix. Repository
governance treats that prefix as a plugin owner marker and would report every
importer as crossing a plugin boundary.

## What it provides

- **Authentication** (`author.go`) — `HCMCredential`, the `Author` interface and
  `HCMAuth`: Basic auth header plus MD5withRSA query signing.
- **Signing** (`sign.go`) — `CanonicalString`, `Sign`, `Verify`,
  `ParsePrivateKey`, `BasicAuthHeader`.
- **Client** (`client.go`) — `NewClient(baseURL, cred)` and `PostJSON`, which
  applies auth and an `apiCode` per request.
- **Employees** (`employee.go`) — `ListEmployees` (paged) and `ListAllEmployees`
  (all pages) over `batch/data`.
- **Reports** (`report.go`) — `GetReportData` with `ReportHeader` / `ReportData`.

## Usage

```go
priv, err := hcm.ParsePrivateKey(pemKey) // config value
cred := hcm.HCMCredential{
    APIKey:        apiKey,
    EntCode:       entCode,
    PrivateKey:    priv,
    OperatorEmail: operatorEmail,
    APICodes:      map[string]string{hcm.APICodeKeyBatchData: apiCode},
}
client := hcm.NewClient(baseURL, cred)
employees, err := client.ListAllEmployees(ctx)
```

Credentials are read from the consuming plugin's own runtime configuration and
are never stored in this library or in the database.

## Dependents

`linapro-employee-core` (employee directory sync) and `linapro-moka-report-sync`
(report to Bitable sync).
