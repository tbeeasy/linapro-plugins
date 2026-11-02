# 飞书消息推送 · 对外接口对接文档

本文面向需要从外部系统（ERP、监控平台、业务系统等）向员工发送飞书消息的技术人员。
调用方只需提供**工号**与**消息内容**，由本服务负责找到员工对应的飞书账号并完成投递；
调用方不需要接触任何飞书应用凭据。


## 1. 接入准备

| 项 | 说明 |
|---|---|
| 接口地址 | `https://<服务域名>/x/linapro-lark-push/open/v1` |
| 协议 | 必须使用 **HTTPS**（请求体不在签名范围内，依赖 TLS 防篡改） |
| 凭据 | `app_key` + `secret`，由管理员在后台「系统设置 → 飞书推送凭据」创建后线下交付 |
| 字符编码 | UTF-8 |
| 时钟 | 调用方服务器须与标准时间同步（允许偏差 ±300 秒） |

每个凭据在后台绑定了：

- **推送分组**：决定消息由哪个飞书机器人发出，调用方无法在请求中指定；
- **群聊（可选）**：若配置了群聊，该凭据的每次推送都会**同时**发到这个群，调用方同样无法指定或关闭。

> `secret` 只用于在本地计算签名，**不要**放进请求、日志或前端代码。凭据泄露请联系管理员停用并重新签发。


## 2. 签名鉴权

所有接口都要求携带以下 4 个请求头：

| 请求头 | 必填 | 说明 |
|---|---|---|
| `X-App-Key` | 是 | 凭据的 `app_key`（公开标识） |
| `X-Timestamp` | 是 | 当前 Unix 时间戳，**秒**级 |
| `X-Nonce` | 是 | 随机串，长度 16~64 字符；**每个请求都必须不同**，同一个 nonce 只能成功使用一次 |
| `X-Signature` | 是 | 签名，小写十六进制，计算方式见下文 |

### 2.1 待签串

按顺序用换行符 `\n` 连接以下 6 行（末尾不加换行）：

```
HTTP方法（大写）
请求路径
规范化查询串
X-Timestamp 的值
X-Nonce 的值
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

- **请求路径**：不含域名与查询串，如 `/x/linapro-lark-push/open/v1/message/text`。
- **规范化查询串**：当前所有接口都没有查询参数，此行**固定为空串**（即连续两个换行）。
  若将来出现查询参数，规则为：每项编码为 `urlencode(名)=urlencode(值)`（空格编码为 `+`），
  按字符串升序排序后用 `&` 连接。
- **第 6 行**：固定值（空串的 SHA-256）。**请求体不参与签名**，无论 JSON 还是文件上传都一样。

### 2.2 计算签名

```
X-Signature = hex( HMAC-SHA256( key = secret, data = 待签串 ) )
```

### 2.3 校验示例

可用下面这组数据核对自己的签名实现：

| 项 | 值 |
|---|---|
| secret | `WHUBwDkuYCE2WyN5u8NFENk24E2EXAMPLE` |
| 方法 / 路径 | `POST` `/x/linapro-lark-push/open/v1/message/text` |
| X-Timestamp | `1761638400` |
| X-Nonce | `8f14e45fceea167a5a36dedd4bea2543` |

待签串（`↵` 表示换行符）：

```
POST↵/x/linapro-lark-push/open/v1/message/text↵↵1761638400↵8f14e45fceea167a5a36dedd4bea2543↵e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
```

正确的签名：

```
ffb1f7efa9ffaa3528e77013769789d7e9214cdc59d9e67c8291437b13ed55ab
```


## 3. 接口列表

| 接口 | 方法 | 路径 | 用途 |
|---|---|---|---|
| 文本推送 | POST | `/message/text` | 发送纯文本消息 |
| 文件推送 | POST | `/message/file` | 发送文件或图片（上传即推送） |
| 自检 | GET | `/status` | 部署验收用，不发送任何消息 |

### 收件人与投递规则（文本、文件通用）

- 收件人用**工号**（`employee_no`）指定，这是推荐方式；已持有飞书企业级 `user_id` 时也可用 `user_id`。
  两者同时填写时以工号为准。
- 一个员工如果在多个飞书企业都有账号，**每个企业各收到一条**。
- 同一请求内重复的收件人自动去重，只发一次。
- 凭据配置了群聊时，消息会额外发到该群；此时 `recipients` 可以为空（只发群）。
  凭据**未**配置群聊时，`recipients` 不能为空。


### 3.1 文本推送

```
POST /x/linapro-lark-push/open/v1/message/text
Content-Type: application/json
```

**请求参数（JSON）**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `recipients` | array | 视情况 | 个人收件人列表，元素结构见下表；凭据未配置群聊时必填且不能为空 |
| `text` | string | 是 | 消息正文，纯文本，支持换行；不能为空，最长 **2000 字符**（超长直接报错，不会截断） |
| `biz_ref` | string | 否 | 业务单号，仅用于双方日志对账，**不具备幂等/去重作用** |

`recipients` 元素：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `employee_no` | string | 二选一 | 员工工号 |
| `user_id` | string | 二选一 | 飞书企业级 user_id |

> 请求体只允许以上字段，出现未知字段（如 `group`、`chat_id`）以参数错误拒绝（`LINAPRO_LARK_PUSH_INVALID_PARAM`）。请求体上限 256 KB，超出返回 HTTP 413。

**请求示例**

```json
{
  "recipients": [
    { "employee_no": "GZ001198" },
    { "employee_no": "GZ001199" }
  ],
  "text": "【预警】产线 3 号机组温度超过阈值\n当前温度：92℃\n请值班人员立即处理。",
  "biz_ref": "ALERT-20261028-0012"
}
```

**响应**：见 [第 4 节](#4-响应说明)。


### 3.2 文件推送

```
POST /x/linapro-lark-push/open/v1/message/file
Content-Type: multipart/form-data
```

**请求参数（表单字段）**

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `file` | 文件 | 是 | 有且只有一个文件，最大 **30 MB** |
| `recipients` | string | 视情况 | 个人收件人，**JSON 数组字符串**，元素结构同文本接口；规则同文本接口 |
| `biz_ref` | string | 否 | 业务单号，仅用于日志对账 |

- 接收方看到的文件名即上传时的文件名。
- 内容识别为图片且不超过 10 MB 时，以**图片消息**发送；其他情况以**文件消息**发送。
- 只允许以上三个字段，出现其他字段以参数错误拒绝；整个请求体上限 31 MB，超出返回 HTTP 413。
- 文件只在内存中中转，服务端不落盘、不保存。

**请求示例**

```
file:        (二进制) 2026年10月产线月报.pdf
recipients:  [{"employee_no":"GZ001198"},{"employee_no":"GZ001199"}]
biz_ref:     REPORT-202610
```

**响应**：见 [第 4 节](#4-响应说明)。大文件上传到飞书较慢，客户端超时建议不少于 120 秒。


### 3.3 自检

```
GET /x/linapro-lark-push/open/v1/status
```

无请求参数。用于部署验收与监控，不会发送任何消息。返回服务端所有推送分组的状态（不限于本凭据的分组）。

**响应示例（HTTP 200）**，下表字段均位于 `data` 内：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "plugin_ready": true,
    "config_valid": true,
    "push_apps": [
      {
        "group": "outer_notify",
        "app_id": "cli_a1b2c3d4e5f6",
        "name": "外网消息通知",
        "identity_app_id": "cli_f6e5d4c3b2a1",
        "credential_ready": true,
        "addressable_employees": 1532,
        "blank_user_id_rows": 0,
        "collisions": 0
      }
    ]
  }
}
```

| 字段 | 说明 |
|---|---|
| `plugin_ready` | 推送能力是否就绪 |
| `config_valid` | 服务端推送配置是否全部合法 |
| `config_issues` | 配置问题清单（仅有问题时出现） |
| `push_apps[].group` | 推送分组名 |
| `push_apps[].app_id` / `name` | 发消息的飞书应用及其名称 |
| `push_apps[].identity_app_id` | 投递时用来查找员工 user_id 的飞书应用 |
| `push_apps[].credential_ready` | 该应用凭据是否齐备 |
| `push_apps[].addressable_employees` | 该企业中可以收到消息的员工数；为 0 说明该企业整体发不出去 |
| `push_apps[].blank_user_id_rows` | 有账号但缺少 user_id 的员工数（通常是服务端权限问题） |
| `push_apps[].collisions` | 工号对应多个账号、被拒绝投递的员工数 |
| `push_apps[].probe_error` | 自检出错原因（仅出错时出现） |

推送能力未就绪时返回 **HTTP 503**，`errorCode` 为 `LINAPRO_LARK_PUSH_SENDER_UNBOUND`，`data` 为 `null`。


## 4. 响应说明

### 4.1 统一响应结构

所有接口的响应体都是同一种 JSON 结构，与本系统后台接口一致。签名失败、参数错误，以及 HTTP 状态码为 413 / 500 / 503 时也一样。

成功：

```json
{
  "code": 0,
  "message": "OK",
  "data": { ... }
}
```

失败时多出 `errorCode` 等字段，`data` 一般为 `null`（部分投递失败除外，见 4.2）：

```json
{
  "code": 53,
  "message": "请求参数非法：text 不能为空",
  "data": null,
  "errorCode": "LINAPRO_LARK_PUSH_INVALID_PARAM",
  "messageKey": "error.linapro.lark.push.invalid.param",
  "messageParams": {
    "item": "text 不能为空"
  }
}
```

| 字段 | 说明 |
|---|---|
| `code` | `0` 表示成功，非 `0` 表示失败；只用来判断成败，数值是错误大类，多种错误可能相同（见 4.3） |
| `errorCode` | 具体错误码（稳定字符串），失败时出现；**程序按它判断具体原因** |
| `message` | 展示用文案，可能是中文或英文、也可能调整，只用于记录日志，不要用于程序判断 |
| `data` | 业务数据；投递结果、自检报告都在这里 |
| `messageKey` / `messageParams` | 文案的多语言键与参数（不是每个错误都有参数），调用方可忽略 |

判断规则：**先看 `code` 是否为 0，不为 0 再看 `errorCode`**。

HTTP 状态码只有三种例外，其余情况一律为 **200**：

| HTTP 状态 | 含义 |
|---|---|
| 413 | 请求体超限 |
| 500 | 服务端推送配置有问题，或其他内部错误 |
| 503 | 服务端能力暂不可用 |

> 调用方的 HTTP 客户端不要只用「状态码是否为 200」判断成功：签名错误、参数错误、部分投递失败都返回 HTTP 200，
> 必须再看 `code`。反过来，413 / 500 / 503 的响应体也是上述 JSON，不要因为状态码不是 200 就丢弃响应体，
> 其中的 `errorCode` 可用于定位原因。

### 4.2 投递结果（文本 / 文件接口）

| 结果 | `code` | `errorCode` | `data` |
|---|---|---|---|
| 全部投递成功 | `0` | — | `{"all_succeeded": true}` |
| 部分或全部收件人投递失败 | 非 `0` | `LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED` | `{"all_succeeded": false, "failed": [...]}` |

**部分失败响应示例（HTTP 200）**

```json
{
  "code": 60,
  "message": "2 个收件人投递失败",
  "data": {
    "all_succeeded": false,
    "failed": [
      {
        "employee_no": "GZ001199",
        "lark_code": 0,
        "lark_msg": "no_identity",
        "lark_req_id": ""
      },
      {
        "employee_no": "GZ001200",
        "lark_code": 230013,
        "lark_msg": "Bot has NO availability to this user.",
        "lark_req_id": "20261028101530C8A3E0F1D2B7A9C4E5F6"
      }
    ]
  },
  "errorCode": "LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED",
  "messageKey": "error.linapro.lark.push.send.partial.failed",
  "messageParams": {
    "count": 2
  }
}
```

`data.failed` 元素字段：

| 字段 | 说明 |
|---|---|
| `employee_no` / `user_id` / `chat_id` | 失败的收件人，按请求时的写法返回其中一个；`chat_id` 表示后台配置的群聊投递失败 |
| `lark_code` | 飞书错误码；非飞书侧原因时为 `0` |
| `lark_msg` | 飞书错误信息；非飞书侧原因时为下表中的原因码 |
| `lark_req_id` | 飞书请求 ID，向飞书反馈问题时提供；非飞书侧原因时为空 |

非飞书侧原因码（`lark_code` 为 0 时）：

| `lark_msg` | 含义 | 处理建议 |
|---|---|---|
| `no_identity` | 该员工在所有企业都没有飞书账号（工号错误或尚未同步） | 核对工号；新员工等通讯录同步后再试 |
| `user_id_missing` | 账号存在但缺少 user_id | 联系服务端管理员 |
| `collision` | 工号对应多个飞书账号，为避免发错人已拒绝投递 | 联系服务端管理员 |
| `failed` | 网络等本地原因失败 | 可重试 |

> 员工只在其中一个企业有账号时，另一个企业「无账号」属于正常情况，**不会**出现在 `failed` 中。

### 4.3 错误码一览

| HTTP 状态 | `code` | `errorCode` | 含义 |
|---|---|---|---|
| 200 | `0` | — | 成功 |
| 200 | `60` | `LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED` | 有收件人投递失败，明细见 `data.failed` |
| 200 | `53` | `LINAPRO_LARK_PUSH_INVALID_PARAM` | 参数非法：JSON 格式错误、含未知字段、正文为空或超长、没有收件人、文件缺失或超 30 MB 等；具体原因见 `message` |
| 200 | `53` | `LINAPRO_LARK_PUSH_SIGNATURE_MISSING` | 缺少签名请求头，或 nonce 长度不在 16~64 之间 |
| 200 | `61` | `LINAPRO_LARK_PUSH_SIGNATURE_UNKNOWN_APP_KEY` | app_key 不存在或已停用 |
| 200 | `61` | `LINAPRO_LARK_PUSH_SIGNATURE_TIMESTAMP_INVALID` | 时间戳格式错误或与服务器时间相差超过 300 秒 |
| 200 | `61` | `LINAPRO_LARK_PUSH_SIGNATURE_MISMATCH` | 签名不正确 |
| 200 | `61` | `LINAPRO_LARK_PUSH_SIGNATURE_NONCE_REPLAYED` | nonce 已被使用过 |
| 413 | `53` | `LINAPRO_LARK_PUSH_BODY_TOO_LARGE` | 请求体超限（文本 256 KB / 文件 31 MB） |
| 500 | `56` | `LINAPRO_LARK_PUSH_NO_PUSH_APP` / `LINAPRO_LARK_PUSH_CONFIG_INVALID` | 服务端推送配置问题 |
| 500 | `50` | 无 | 服务端内部错误（响应中没有 `errorCode`） |
| 503 | `58` | `LINAPRO_LARK_PUSH_CREDENTIAL_STORE_UNAVAILABLE` / `LINAPRO_LARK_PUSH_NONCE_STORE_UNAVAILABLE` | 签名校验依赖暂不可用 |
| 503 | `58` | `LINAPRO_LARK_PUSH_SENDER_UNBOUND` / `LINAPRO_LARK_PUSH_EMPCAP_UNBOUND` / `LINAPRO_LARK_PUSH_HOST_CONFIG_MISSING` | 推送能力暂不可用 |

> 签名校验先于请求体检查：签名不通过的请求不会读取请求体，因此超大请求在签名错误时返回的是签名类错误，而不是 413。
>
> 服务未部署、路径写错或推送插件未启用时，服务端返回 **HTTP 404 且响应体为空**（不是上面的 JSON），请按「服务不可用」处理。

### 4.4 重试建议

| 情况 | 建议 |
|---|---|
| `code` = 0 | 完成，无需处理 |
| `SEND_PARTIAL_FAILED` | 只针对 `data.failed` 中的收件人重发。注意：员工在多个企业有账号且只有其中一个企业失败时，重发会让已收到的企业再收一条 |
| `INVALID_PARAM` / HTTP 413 | 修正请求后再发，原样重试无效 |
| `SIGNATURE_*` | 检查凭据、签名与服务器时钟；重试时必须重新生成 timestamp、nonce 与签名 |
| HTTP 500 / 503 / 网络超时 | 间隔递增重试（如 1s、5s、30s）。超时的请求可能已经送达，接口**没有幂等保护**，重试可能导致重复消息 |

## 5. 代码示例（PHP）

以下代码演示签名与三个接口的调用方式，可按需改写到自己的项目中。依赖：PHP 7.4+，curl 扩展。

### 5.1 签名

```php
<?php
const LARK_PUSH_PREFIX = '/x/linapro-lark-push/open/v1';
// 请求体不参与签名，待签串最后一行固定为空串的 SHA-256。
const LARK_PUSH_EMPTY_BODY_DIGEST = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855';

/**
 * 生成签名请求头。当前接口都没有查询参数，规范化查询串固定为空。
 */
function larkPushSignHeaders(string $method, string $path, string $appKey, string $secret): array
{
    $timestamp = (string) time();
    $nonce = bin2hex(random_bytes(16)); // 32 个字符，每次请求都要重新生成

    $stringToSign = implode("\n", [
        strtoupper($method),
        $path,
        '',                 // 规范化查询串
        $timestamp,
        $nonce,
        LARK_PUSH_EMPTY_BODY_DIGEST,
    ]);

    return [
        'X-App-Key: ' . $appKey,
        'X-Timestamp: ' . $timestamp,
        'X-Nonce: ' . $nonce,
        'X-Signature: ' . hash_hmac('sha256', $stringToSign, $secret),
    ];
}
```

### 5.2 发送请求

```php
/**
 * 发送请求，返回 [HTTP 状态码, 解码后的响应]。
 * $body 为 JSON 字符串、multipart 字段数组或 null（GET）。
 */
function larkPushRequest(string $method, string $url, array $headers, $body, int $timeout): array
{
    $ch = curl_init($url);
    curl_setopt_array($ch, [
        CURLOPT_CUSTOMREQUEST  => $method,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT        => $timeout,
        CURLOPT_HTTPHEADER     => array_merge($headers, ['Expect:']),
    ]);
    if ($body !== null) {
        curl_setopt($ch, CURLOPT_POSTFIELDS, $body);
    }
    $raw = curl_exec($ch);
    if ($raw === false) {
        $err = curl_error($ch);
        curl_close($ch);
        throw new RuntimeException('飞书推送请求失败：' . $err);
    }
    $status = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);
    return [$status, json_decode($raw, true)];
}
```

### 5.3 文本推送

```php
$baseUrl = 'https://push.example.com';
$appKey  = '<app_key>';
$secret  = '<secret>';

$path = LARK_PUSH_PREFIX . '/message/text';
$body = json_encode([
    'recipients' => [
        ['employee_no' => 'GZ001198'],
        ['employee_no' => 'GZ001199'],
    ],
    'text'    => "【预警】产线 3 号机组温度超过阈值\n当前温度：92℃",
    'biz_ref' => 'ALERT-20261028-0012',
], JSON_UNESCAPED_UNICODE);

$headers   = larkPushSignHeaders('POST', $path, $appKey, $secret);
$headers[] = 'Content-Type: application/json';

[$status, $res] = larkPushRequest('POST', $baseUrl . $path, $headers, $body, 15);

if (!is_array($res)) {
    // 响应不是 JSON（如服务未部署时的空 404），按服务不可用处理：稍后重试
} elseif ($res['code'] === 0) {
    // 全部送达
} elseif (($res['errorCode'] ?? '') === 'LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED') {
    foreach ($res['data']['failed'] as $item) {
        // 记录失败收件人，按需重发：$item['employee_no']、$item['lark_msg']、$item['lark_req_id']
    }
} else {
    // 其他失败：按 $res['errorCode'] 处理（服务端内部错误时没有该字段），$res['message'] 记日志；
    // $status 为 413 时修正请求，为 500 / 503 时稍后重试（见 4.4）
}
```

### 5.4 文件推送

```php
$path = LARK_PUSH_PREFIX . '/message/file';
$fields = [
    'file'       => new CURLFile('/data/report/2026-10.pdf', 'application/octet-stream', '2026年10月产线月报.pdf'),
    'recipients' => json_encode([['employee_no' => 'GZ001198']]), // 注意是 JSON 字符串
    'biz_ref'    => 'REPORT-202610',
];

// 不要手动设置 Content-Type，curl 会自动生成带 boundary 的 multipart/form-data。
$headers = larkPushSignHeaders('POST', $path, $appKey, $secret);

[$status, $res] = larkPushRequest('POST', $baseUrl . $path, $headers, $fields, 120);
// 响应处理同文本推送
```

### 5.5 自检

```php
$path    = LARK_PUSH_PREFIX . '/status';
$headers = larkPushSignHeaders('GET', $path, $appKey, $secret);

[$status, $res] = larkPushRequest('GET', $baseUrl . $path, $headers, null, 15);
// $res['code'] === 0 时：$res['data']['plugin_ready']、$res['data']['push_apps'][*]['addressable_employees']
```


## 6. 常见问题

**Q：签名总是 `SIGNATURE_MISMATCH`？**
依次检查：路径是否包含 `/x/linapro-lark-push/open/v1` 前缀且不含域名；第 3 行是否为空串（两个换行连续）；
第 6 行是否为固定值；签名是否为小写十六进制；`secret` 是否有多余空格。可用第 2.3 节的示例数据核对。

**Q：返回 `TIMESTAMP_INVALID`？**
时间戳必须是**秒**（不是毫秒），且调用方服务器时间与标准时间相差不超过 300 秒。

**Q：返回 `NONCE_REPLAYED`？**
每次请求（包括重试）都必须重新生成 nonce 并重新计算签名，不能复用上一次的请求头。

**Q：`code` 为 0 但员工说没收到？**
检查员工飞书里是否屏蔽或删除了该机器人会话；如果问题持续，带上 `biz_ref` 和请求时间联系服务端管理员查日志。

**Q：能不能指定由哪个机器人发送，或发到某个群？**
不能在请求中指定。机器人与群聊都由凭据在后台的配置决定，如需调整请联系管理员。
