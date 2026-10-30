# linapro-moka-hcm

Moka HCM OpenAPI 的精简 Go 客户端，供需要访问 Moka 的源码插件共用。它是一个**共享库**，不是插件：没有`plugin.yaml`、不做宿主注册、不建表、不注册任务，通过 go.mod 的`require`+`replace`引入。

模块名有意不带`lina-plugin-`前缀——仓库治理把该前缀当作插件 owner 标记，带上会让每个引入方都被判定为跨插件越界。

## 它提供什么

- **认证**（`author.go`）：`HCMCredential`、`Author`接口与`HCMAuth`——Basic 认证头 + MD5withRSA 查询签名。
- **签名**（`sign.go`）：`CanonicalString`、`Sign`、`Verify`、`ParsePrivateKey`、`BasicAuthHeader`。
- **客户端**（`client.go`）：`NewClient(baseURL, cred)`与`PostJSON`，按请求应用认证与`apiCode`。
- **员工**（`employee.go`）：`ListEmployees`（分页）与`ListAllEmployees`（全量分页），走`batch/data`接口。
- **报表**（`report.go`）：`GetReportData`，返回`ReportHeader`/`ReportData`。

## 用法

```go
priv, err := hcm.ParsePrivateKey(pemKey) // 来自配置
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

凭据由使用方插件从自己的运行期配置读取，本库与数据库都不保存凭据。

## 使用方

`linapro-employee-core`（员工目录同步）与`linapro-moka-report-sync`（报表同步到多维表格）。
