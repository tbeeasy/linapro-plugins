# linapro-moka-recruit

Moka 招聘 OpenAPI 的 Go 客户端，供需要访问 Moka 招聘系统的源码插件共用。它是一个**共享库**，不是插件：没有`plugin.yaml`、不做宿主注册、不建表、不注册任务，通过`go.mod`的`require`+`replace`引入。

模块名有意不带`lina-plugin-`前缀——仓库治理把该前缀当作插件 owner 标记，带上会让每个引入方都被判定为跨插件越界。

## 认证方式

两种方式都实现`Author`接口，注入`NewClient`使用。

| 方式 | 构造函数 | 行为 |
|------|----------|------|
| `BasicAuth` | `NewBasicAuth(BasicCredential{APIKey})` | `Authorization: Basic base64(apiKey:)`，招聘系统主力方式 |
| `OAuth2Auth` | `NewOAuth2Auth(baseURL, OAuth2Credential{ClientID, ClientSecret})` | `client_credentials`换取 Bearer token，自动续期 |

## API 路径前缀

所有接口都位于`DefaultBaseURL`（`https://api.mokahr.com`）的`/api-platform/`之下。当前已封装的接口使用`/api-platform/v1/`、`/api-platform/v2/`、`/api-platform/candidate/v1/`和`/api-platform/application/`前缀；其他招聘接口可传完整路径经`PostJSON`、`GetJSON`或`PutQuery`调用。

## 它提供什么

- **传输层**（`client.go`）：`NewClient(baseURL, auth)`、`PostJSON`、`GetJSON`、`PutQuery`。
- **应聘记录**（`application.go`）：`EhrApplications`、`MoveApplicationStage`、`GetInterviewInformation`、`GetHandleResult`。
- **简历**（`resume.go`）：`GetResumeContent`。
- **阶段**（`stage.go`）：`GetStagesList`。
- **报表**（`report.go`）：`GetReportData`，返回`ReportHeader`/`ReportData`。

## 用法

```go
import "linapro-moka-recruit/moka"

auth := moka.NewBasicAuth(moka.BasicCredential{APIKey: apiKey}) // 来自配置
client := moka.NewClient(baseURL, auth) // baseURL 为空时回退到 DefaultBaseURL
report, err := client.GetReportData(ctx, reportID)
```

使用方`go.mod`需要：

```text
require linapro-moka-recruit v0.0.0
replace linapro-moka-recruit => ../linapro-moka-recruit
```

凭据由使用方插件从自己的运行期配置读取，本库与数据库都不保存凭据。

## 使用方

`linapro-moka-report-sync`（报表同步到多维表格）与`linapro-recruit-pipeline`（招聘流水线编排）。
