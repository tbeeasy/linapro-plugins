# linapro-lark-push

飞书消息推送能力插件（文本、文件 / 图片 + 进程内交互卡片；收件人可以是人或群聊）。唯一的页面是
「系统设置」下的签名凭据管理页。

它回答一个问题：**任意插件或外部系统，怎么给「一个人」或「一个群」发一条飞书消息？** 发给人时
调用方只给工号，系统自己去查这个人在哪几个飞书企业有身份，然后用**调用方自己分组**里各企业的
**推送应用**（不是登录/考勤应用）机器人各发一条。不同调用方用不同分组，接收方看到的机器人
名也就不同（如「360测评助手」与「考勤助手」）。

## 能力边界

做：

- 进程内门面 `backend/cap/larkpushcap`：其它插件以「工号/ user_id + 文本」请求投递，或以
  「工号/ user_id + 卡片构造函数」投递交互卡片（如 `linapro-eval-360` 的测评模板卡片）；
- 对外 HTTP 接口 `POST /x/linapro-lark-push/open/v1/message/text` 与
  `POST /x/linapro-lark-push/open/v1/message/file`（一步式上传即推送），插件自建 HMAC 签名鉴权；
- 群聊：对外接口的群聊目标在后台配置在签名凭据上（调用方不能指定 `chat_id`）；进程内门面可直接以
  `chat_id` 为收件人（文本、文件与卡片）；
- 后台 **系统设置 → 飞书推送凭据** 管理签名凭据；
- 能力自检端点 `GET /x/linapro-lark-push/open/v1/status`（部署验收与告警）；
- 失败项返回（收件人 + 飞书错误码 / 信息 / 请求 ID），逐收件人 × 逐推送应用的明细记入结构化日志。

不做（本期）：

- 对外 HTTP 发卡片、富文本、@ 人（对外接口做文本与文件；卡片仅进程内）；
- 投递记录落库、失败重试队列、必达语义——失败只记日志并返回失败项，由调用方重试；
- 宿主站内信（不写 `notifycap`、不进 `usermsg`）；
- 由系统替调用方选择「发给哪个企业」——按身份自动扇出，双发是预期行为。

## 三条硬约束

1. **投递凭据只取请求分组的 `push_groups.<分组>`。** 发送循环遍历的只有该分组的推送应用；
   `identity_app_id` 只是一个用来查 `lark_identity` 的 app_id 字符串——身份应用的密钥本插件
   读不到、也不需要，它从不进入发送路径。这从结构上杜绝「告警走考勤机器人」。
2. **分组必填、不回退。** 进程内调用必须传 `Group`（填自己的插件 ID），对外 HTTP 的分组由
   签名凭据绑定。分组为空是参数错误；分组未配置返回「该分组无可用推送应用」——**绝不改用
   其它分组或全部推送应用**。
3. **一次请求可能发出多条。** 同一个人在两家企业都有身份就发两条（两个企业的机器人不同，
   用户视角是两套系统各通知一次）。结果明细里逐条给出企业、状态与飞书 message_id。

## 配置

### 飞书推送凭据（`plugin.linapro-lark-push.push_groups`）

推送应用凭据是本插件的**私有配置**，按调用方分组：键是分组名，值是推送应用数组（每个参与
投递的飞书企业一条）。分组名 = 调用方插件 ID；对外 HTTP 调用方的分组名自定，由签名凭据绑定。
登录 / 通讯录同步应用属于 `linapro-employee-core`（`plugin.linapro-employee-core.lark_apps`），
本插件不读它的密钥。

```yaml
plugin:
  linapro-lark-push:
    push_groups:
      linapro-eval-360:          # 分组名 = 调用方插件 ID
        - app_id: cli_push_a     # 独立的推送应用，不要复用登录应用
          secret_key: "xxx"
          name: "360测评助手(霍尔果斯)"
          identity_app_id: cli_xxx   # 填 employee-core lark_apps 里同企业的 app_id
      linapro-attend-poc:        # 考勤例外：直接用登录应用（即考勤机器人）本身发送
        - app_id: cli_xxx
          secret_key: "与 lark_apps 中 cli_xxx 相同"
          name: "考勤(霍尔果斯)"
          identity_app_id: cli_xxx
      ops-alert:                 # 对外 HTTP 用的分组，名字自定
        - app_id: cli_push_d
          secret_key: "zzz"
          name: "运维告警(霍尔果斯)"
          identity_app_id: cli_xxx
```

校验规则（每次发送前只校验请求分组；`status` 自检校验全部分组）：

- 组内 `app_id` / `secret_key` / `identity_app_id` 非空；
- 组内 `app_id` 唯一；
- 组内同一个 `identity_app_id` 不得被两个推送应用引用（否则同一人在同一企业收到两条）；
- 同一个 `app_id` **可以**出现在多个分组（多个调用方共用一个机器人）；
- 某分组非法只让该分组不可用，其它分组照常投递；只有 `push_groups` 整体结构无法解析时
  全部不可用。

所需飞书应用数 = 分组数 × 企业数（除非多个分组共用应用）。

`identity_app_id` 指向 employee-core 配置里的身份应用，本插件看不到那份配置，故**悬空
引用无法在配置期识别**——表现为 `status` 里该推送应用的 `addressable_employees` 为 0。

### 签名凭据（后台「系统设置 → 飞书推送凭据」）

对外 HTTP 接口用 **HMAC-SHA256 签名**鉴权。凭据存放在本插件数据表
`plugin_linapro_lark_push_api_key`，在后台 **系统设置 → 飞书推送凭据** 页维护（不再有配置键）。
每条凭据为 `{名称, app_key, secret, 分组, 群聊 chat_id, 群聊推送应用, 状态, 备注}`：

- `app_key` 是**公开标识**——放在 `X-App-Key` 头里传输，会进日志；1~64 个字母、数字、`_`、`-`、`.`，
  在未删除的凭据中唯一（删除后同名可重建）。
- `secret` 是签名密钥，**永不上网络**：调用方用它本地计算签名，服务端用它重算比对，且不进日志。
  取值为 16~128 个可见 ASCII 字符。新增时必填：管理页打开新增表单即在浏览器本地预填 64 位字母数字
  随机串，可修改或点「生成」重新生成，留空不允许提交，服务端同样拒绝空值、不代为生成；编辑时留空
  保持原值。列表与详情接口不返回 `secret`，查看需单独的「查看密钥」权限。
- 分组绑定 `push_groups` 中已配置的一个推送分组：投递分组由命中的凭据决定，请求体不能指定分组
  （带 `group` 字段会以未知字段被拒，返回 `LINAPRO_LARK_PUSH_INVALID_PARAM`），持凭据者无法冒用其它分组的机器人。
- 群聊 `chat_id`（可选，`oc_` 开头）：配置后，以该凭据签名的**每次**文本 / 文件推送都会同时发到这个群；
  调用方不能在请求里指定群聊（带 `chats` 字段以参数错误被拒），避免持凭据者把消息发到任意群。
  不同的群用不同的凭据。
- 群聊推送应用（可选）：由分组内哪个推送应用（机器人）发到群里。分组只有一个推送应用时可不选，有多个
  时必选，且必须属于该分组。所选机器人须已在群内，否则群聊明细为 `failed`。
- 状态使用宿主字典 `sys_normal_disable`（`1` 正常、`0` 停用）。

签名中间件**每次请求都按 `app_key` 查库**（命中唯一索引、不加缓存），所以停用、删除或修改凭据在
下一次请求即生效。轮换方式：给调用方新建一条凭据，待其切换完成后停用或删除旧凭据。

| 权限点 | 含义 |
|---|---|
| `linapro-lark-push:api-key:list` | 进入页面、查看凭据列表与详情、读取推送分组 |
| `linapro-lark-push:api-key:add` | 新增凭据 |
| `linapro-lark-push:api-key:edit` | 编辑、启停凭据 |
| `linapro-lark-push:api-key:remove` | 删除凭据（软删除） |
| `linapro-lark-push:api-key:secret` | 查看 `secret` 明文 |

## 签名请求

### 请求头

| 请求头 | 含义 |
|---|---|
| `X-App-Key` | 公开的凭据标识 |
| `X-Timestamp` | Unix 时间戳，单位**秒**；须在服务器时间 **±300 秒**内 |
| `X-Nonce` | 随机串，**16~64 字符**；每条请求必须不同 |
| `X-Signature` | 小写十六进制 HMAC-SHA256 签名 |

### 待签串

```text
METHOD + "\n" + PATH + "\n" + 规范化查询串 + "\n" + TIMESTAMP + "\n" + NONCE + "\n" + hex(SHA256(""))
```

- `METHOD`——大写 HTTP 方法；
- `PATH`——不含查询串的请求路径，如 `/x/linapro-lark-push/open/v1/message/text`；
- `规范化查询串`——查询参数按参数名、再按值排序，每项渲染为 `urlencode(name)=urlencode(value)`
  （空格为 `+`），用 `&` 连接；无参数时为空串；
- 末行——**所有接口的请求体都不参与签名**，末行固定为空串的 SHA-256：
  `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`。保留这一行只为与既有
  客户端的待签串结构一致；调用方无需为签名读取或摘要请求体（文件接口不必把整个文件读进内存）。

签名覆盖方法、路径、查询串、时间戳与 nonce，改动任一项都会失效。**请求体（消息内容、文件、收件人）
不受签名保护，生产环境必须走 HTTPS**。被截获的请求只能在
时间戳窗口内被重放，而 nonce 连这个窗口也堵死：**同一 `app_key` 的同一 `nonce` 最多成功一次**。

### Go 示例

```go
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/util/guid"
)

// sign 计算对外推送接口要求的 HMAC-SHA256 签名。
func sign(method, rawURL, appKey, secret string, body []byte) (*http.Request, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := strings.ReplaceAll(guid.S(), "-", "") // 32 字符，满足 16~64

	// 规范化查询串：按参数名、再按值排序，逐项 urlencode 后用 & 连接。
	pairs := make([]string, 0, len(u.Query()))
	for name, values := range u.Query() {
		if len(values) == 0 {
			pairs = append(pairs, url.QueryEscape(name)+"=")
			continue
		}
		for _, value := range values {
			pairs = append(pairs, url.QueryEscape(name)+"="+url.QueryEscape(value))
		}
	}
	sort.Strings(pairs)

	// 请求体不参与签名：末行固定为空串的 SHA-256。
	const emptyBodyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	stringToSign := strings.Join([]string{
		strings.ToUpper(method),
		u.Path,
		strings.Join(pairs, "&"),
		timestamp,
		nonce,
		emptyBodyDigest,
	}, "\n")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stringToSign))

	req, err := http.NewRequest(method, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-App-Key", appKey)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func main() {
	const (
		endpoint = "https://<host>/x/linapro-lark-push/open/v1/message/text"
		appKey   = "ops-alert-prod"
		secret   = "<secret>"
	)
	body := []byte(`{"recipients":[{"employee_no":"GZ001198"}],"text":"预警：产线 3 号机组温度超过阈值","biz_ref":"alert-20261021-001"}`)

	req, err := sign(http.MethodPost, endpoint, appKey, secret, body)
	if err != nil {
		panic(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	fmt.Println(resp.StatusCode, string(out))
}
```

### PHP 示例

完整可运行的示例（自检、文本、文件、群聊，PHP ≥ 7.4，需 curl 扩展）见
[`hack/examples/php/lark-push-demo.php`](hack/examples/php/lark-push-demo.php)，下面是文本接口的最小片段：

```php
<?php
declare(strict_types=1);

/**
 * 计算对外推送接口要求的 HMAC-SHA256 签名并发送请求。
 *
 * 待签串 = METHOD + "\n" + PATH + "\n" + 规范化查询串 + "\n" + TIMESTAMP + "\n" + NONCE + "\n" + hash('sha256', '')
 * 请求体不参与签名，末行固定为空串的 SHA-256。
 */
function larkPushSendText(
    string $baseUrl,
    string $appKey,
    string $secret,
    array $payload,
    int $timeoutSeconds = 10
): array {
    $method    = 'POST';
    $path      = '/x/linapro-lark-push/open/v1/message/text';
    $body      = json_encode($payload, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
    $timestamp = (string) time();
    // 32 个十六进制字符，满足 16~64 的长度要求；每条请求都必须不同。
    $nonce     = bin2hex(random_bytes(16));

    // 规范化查询串：本例无查询参数，故为空串。
    $canonicalQuery = '';

    $stringToSign = implode("\n", [
        $method,
        $path,
        $canonicalQuery,
        $timestamp,
        $nonce,
        hash('sha256', ''), // 请求体不参与签名，固定为空串摘要
    ]);

    $signature = hash_hmac('sha256', $stringToSign, $secret);

    $ch = curl_init($baseUrl . $path);
    curl_setopt_array($ch, [
        CURLOPT_POST           => true,
        CURLOPT_POSTFIELDS     => $body,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT        => $timeoutSeconds,
        CURLOPT_HTTPHEADER     => [
            'Content-Type: application/json',
            'X-App-Key: ' . $appKey,
            'X-Timestamp: ' . $timestamp,
            'X-Nonce: ' . $nonce,
            'X-Signature: ' . $signature,
        ],
    ]);

    $responseBody = curl_exec($ch);
    if ($responseBody === false) {
        $error = curl_error($ch);
        curl_close($ch);
        throw new RuntimeException('请求失败: ' . $error);
    }
    $status = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);

    return ['status' => $status, 'body' => json_decode((string) $responseBody, true)];
}

try {
    $result = larkPushSendText(
        'https://<host>',
        'ops-alert-prod',
        '<secret>',
        [
            'recipients' => [['employee_no' => 'GZ001198']],
            'text'       => '预警：产线 3 号机组温度超过阈值',
            'biz_ref'    => 'alert-20261021-001',
        ]
    );
    // 响应为统一结构：code=0 全部成功；errorCode=LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED 时
    // data.failed 列出失败的收件人（只重试这些）。
    echo $result['status'], PHP_EOL;
    print_r($result['body']);
} catch (Throwable $e) {
    fwrite(STDERR, $e->getMessage() . PHP_EOL);
    exit(1);
}
```

> 若接口将来带查询参数：按参数名、再按值排序，对名字与值分别 `urlencode()`（PHP 的
> `urlencode()` 与 Go 的 `url.QueryEscape()` 一致，空格都是 `+`），再用 `&` 连接，即得到
> `规范化查询串`。只用 `http_build_query()` 不够，因为它不排序。

### curl / shell 示例

```bash
HOST='https://<host>'
APP_KEY='ops-alert-prod'
SECRET='<secret>'
PATH_ONLY='/x/linapro-lark-push/open/v1/message/text'
BODY='{"recipients":[{"employee_no":"GZ001198"}],"text":"预警：产线 3 号机组温度超过阈值"}'

TS=$(date +%s)
NONCE=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
EMPTY_BODY_HASH='e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855' # 请求体不参与签名，末行固定为空串摘要
STRING_TO_SIGN=$(printf '%s\n%s\n%s\n%s\n%s\n%s' 'POST' "$PATH_ONLY" '' "$TS" "$NONCE" "$EMPTY_BODY_HASH")
SIGNATURE=$(printf '%s' "$STRING_TO_SIGN" | openssl dgst -sha256 -hmac "$SECRET" -r | cut -d' ' -f1)

curl -sS -X POST "$HOST$PATH_ONLY" \
  -H "Content-Type: application/json" \
  -H "X-App-Key: $APP_KEY" \
  -H "X-Timestamp: $TS" \
  -H "X-Nonce: $NONCE" \
  -H "X-Signature: $SIGNATURE" \
  -d "$BODY"
```

计算签名时必须用 `printf '%s'` 而不是 `echo`：`echo` 会补一个换行符，改变待签串。

### 响应结构与错误码

所有对外接口的响应都使用与后台接口一致的统一结构（FB-12）：

```json
{"code": 0, "message": "OK", "data": {...}}
{"code": 61, "message": "Signature mismatch", "data": null,
 "errorCode": "LINAPRO_LARK_PUSH_SIGNATURE_MISMATCH", "messageKey": "error.linapro.lark.push.signature.mismatch"}
```

- 判断成败看 `code`：`0` 为成功，非 `0` 为失败；
- 判断具体原因看 `errorCode`（稳定字符串）；`message` 只用于展示，文案可能调整；
- HTTP 状态码只有三类例外：请求体超限 **413**、服务端推送配置问题 **500**、服务端能力不可用 **503**；
  其余（含参数错误、签名错误、部分投递失败）一律 **200**。

| HTTP | `errorCode` | 原因 |
|---|---|---|
| 200 | `LINAPRO_LARK_PUSH_INVALID_PARAM` | 参数错误：JSON 非法或含未知字段、正文为空或超长、缺收件人、文件表单非法 |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_MISSING` | 缺签名头，或 `X-Nonce` 长度不在 16~64 |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_UNKNOWN_APP_KEY` | `X-App-Key` 不对应任何启用且未删除的凭据 |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_TIMESTAMP_INVALID` | 时间戳无法解析或超出 ±300 秒——检查调用方时钟 |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_MISMATCH` | 签名不匹配——路径、查询串、方法、时间戳或 nonce 与签名不一致，或仍按请求体摘要计算末行 |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_NONCE_REPLAYED` | 同一 `app_key` 已经用过这个 `nonce` |
| 200 | `LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED` | 有收件人投递失败，失败明细在 `data.failed`（见下文） |
| 413 | `LINAPRO_LARK_PUSH_BODY_TOO_LARGE` | 请求体超过上限：文本接口 256 KB、文件接口 31 MB |
| 500 | `LINAPRO_LARK_PUSH_NO_PUSH_APP` / `LINAPRO_LARK_PUSH_CONFIG_INVALID` | 凭据绑定的分组无可用推送应用，或推送配置非法 |
| 503 | `LINAPRO_LARK_PUSH_CREDENTIAL_STORE_UNAVAILABLE` | 签名凭据表无法查询——fail-closed，不发送任何消息 |
| 503 | `LINAPRO_LARK_PUSH_NONCE_STORE_UNAVAILABLE` | 去重缓存不可用——fail-closed，绝不「跳过去重放行」 |
| 503 | `LINAPRO_LARK_PUSH_SENDER_UNBOUND` / `LINAPRO_LARK_PUSH_EMPCAP_UNBOUND` | 推送能力或员工能力未就绪 |

插件未启用时，宿主直接返回 404（非 JSON），属宿主行为。

签名校验在登记 `nonce` **之前**执行，因此签名错误的请求不会消耗 nonce——攻击者无法烧掉别人
正在传输中的 nonce。调用方时钟须与服务器保持 ±300 秒以内（建议 NTP 对时）。

## 对外接口

```bash
# 见上面「Go 示例」「PHP 示例」「curl / shell 示例」中的签名请求。
```

投递结果在统一结构的 `data` 中，只给调用方结论与失败项；推送应用、message_id、错误详情等逐条明细只记在
服务端日志（按 `request_id` / `biz_ref` 检索）。

全部成功 → `code` 为 `0`：

```json
{"code": 0, "message": "OK", "data": {"all_succeeded": true}}
```

存在失败 → HTTP **200**、`code` 非 `0`、`errorCode` 为 `LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED`，`data.failed`
列出失败项，调用方据此只重试这些收件人：

```json
{
  "code": 60,
  "message": "3 个收件人投递失败",
  "errorCode": "LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED",
  "data": {
    "all_succeeded": false,
    "failed": [
      {"employee_no": "GZ001198", "lark_code": 230013, "lark_msg": "Bot has NO availability to this user.",
       "lark_req_id": "20261021..."},
      {"chat_id": "oc_xxx", "lark_code": 230001, "lark_msg": "...", "lark_req_id": "20261021..."},
      {"employee_no": "GZ009999", "lark_code": 0, "lark_msg": "no_identity", "lark_req_id": ""}
    ]
  }
}
```

| 字段 | 含义 |
|---|---|
| `employee_no` / `user_id` / `chat_id` | 失败的收件人，按请求里的寻址方式给出其一 |
| `lark_code` | 飞书错误码；非飞书侧原因为 `0` |
| `lark_msg` | 飞书错误信息；非飞书侧原因时为下表中的原因 |
| `lark_req_id` | 飞书请求 ID，向飞书反馈问题时使用；非飞书侧原因时为空 |

非飞书侧原因（`lark_code` 为 `0`）：

| `lark_msg` | 含义 | 处置 |
|---|---|---|
| `no_identity` | 该员工在**所有**企业都没有 own 身份（只在部分企业没有身份是正常的，不算失败）；工号写错或不存在也是此状态 | 核对工号或身份同步 |
| `user_id_missing` | own 身份存在但 `lark_user_id` 为空 | **多半是身份应用缺「获取用户 user_id」权限**，或该行待下次同步回填 |
| `collision` | 同一工号在某企业命中多条 own 身份 | 数据问题，拒绝发送以避免送错人 |
| `failed` | 网络失败等未拿到飞书响应 | 可重试 |

一个人在多家企业有身份时，各企业的失败分别列出（同一工号可能出现多次）；重试该工号会向他所有
企业再发一次，已送达的那家会重复。

群聊（文本与文件接口通用）：**请求里不传群聊**。凭据在后台配置了群聊 `chat_id` 时，服务端在
`recipients` 之外自动追加这个群，经凭据指定的推送应用投递一次（群聊不按企业扇出）。此时
`recipients` 可省略（只发群）；凭据未配置群聊时 `recipients` 必填，为空返回 `LINAPRO_LARK_PUSH_INVALID_PARAM`。

```json
{
  "text": "预警：产线 3 号机组温度超过阈值"
}
```

群聊的失败项带 `chat_id`，不带 `employee_no` / `user_id`。

### 文件推送（`POST /x/linapro-lark-push/open/v1/message/file`）

一步式：上传即推送。请求体为 `multipart/form-data`，只接受下列字段（出现其它字段以参数错误被拒）：

| 字段 | 必填 | 含义 |
|---|---|---|
| `file` | 是 | 文件，不超过 30 MB |
| `recipients` | 视情况 | JSON 数组字符串，元素同文本接口；凭据配置了群聊时可省略（只发群） |
| `biz_ref` | 否 | 业务单号，仅用于日志对账 |

按内容识别为 JPEG / PNG / GIF / WEBP / BMP / ICO 且不超过 10 MB 的图片以图片消息发送；其余以文件消息
发送，`file_type` 按扩展名取 `pdf`、`doc`、`xls`、`ppt`、`mp4`、`opus`，其它为 `stream`。飞书的
文件 key 归属上传它的应用，因此对每个「至少有一条可投递明细」的推送应用各上传一次；某应用上传失败
只影响该应用下的明细。响应结构与文本接口一致。本接口请求体上限 31 MB，宿主 `sys.upload.maxSize`
不得低于 30 MB。

签名方式与文本接口相同（请求体不参与签名），multipart 交给 HTTP 客户端照常组装即可。PHP 写法见
[`hack/examples/php/lark-push-demo.php`](hack/examples/php/lark-push-demo.php) 的 `larkPushSendFile`；
Go 写法（复用上面 Go 示例中的 `sign`）：

```go
var body bytes.Buffer
w := multipart.NewWriter(&body)
_ = w.WriteField("recipients", `[{"employee_no":"GZ001198"}]`) // 群聊由凭据配置，不在请求里传
part, _ := w.CreateFormFile("file", "月报.pdf")
_, _ = part.Write(fileBytes)
_ = w.Close()

req, err := sign(http.MethodPost, "https://<host>/x/linapro-lark-push/open/v1/message/file",
    appKey, secret, body.Bytes())
if err != nil {
    panic(err)
}
req.Header.Set("Content-Type", w.FormDataContentType()) // 覆盖 sign 里设置的 application/json
```

自检：

```bash
# GET 请求体为空，签名方式与上面一致（见 Go / PHP / curl 示例）。
```

`status` 端点与 `message/text` 的签名方式完全相同：同样的请求头、同样的待签串。

按「分组 × 推送应用」返回凭据齐备性（每条带 `group`）、`identity_app_id` 有效性、可寻址
人数、空 `user_id` 行数与碰撞数；配置问题项带分组路径（如 `push_groups.linapro-eval-360[0].secret_key`）。**部署验收要求**：该端点全绿 + 两个企业各收到一条 + 机器人名是推送应用。

## 进程内调用

```go
import "lina-plugin-linapro-lark-push/backend/cap/larkpushcap"

res, err := larkpushcap.SendText(ctx, larkpushcap.SendTextRequest{
    Group:      "my-plugin", // 必填：填自己的插件 ID，只用 push_groups.my-plugin 的推送应用
    Recipients: []larkpushcap.Recipient{{EmployeeNo: "GZ001198"}},
    Text:       "预警：产线 3 号机组温度超过阈值",
    BizSource:  "my-plugin",
    BizRef:     "alert-001",
})
```

进程内调用方（同进程的插件代码）可直接以群聊为收件人：群聊是带 `ChatID` 的 `Recipient`（分组有多个
推送应用时再带 `PushAppID`）；文件走 `SendFile`。这与对外接口不同——对外接口的群聊只能来自凭据配置。

```go
res, err := larkpushcap.SendFile(ctx, larkpushcap.SendFileRequest{
    Group: "my-plugin",
    Recipients: []larkpushcap.Recipient{
        {EmployeeNo: "GZ001198"},
        {ChatID: "oc_xxx", PushAppID: "cli_push_a"},
    },
    FileName:  "月报.pdf",
    Content:   pdfBytes,
    BizSource: "my-plugin",
})
```

交互卡片（仅进程内）：卡片内容由调用方按「推送应用 × 收件人」构造——飞书卡片模板须授权给
实际发送的推送应用、跨企业不可复用，链接若要引导员工按企业免登，应携带
`PushApp.IdentityAppID`（员工端换票用身份应用，不是推送应用）。构造返回错误时只记该条为
`failed`，不影响同批其余投递。

```go
apps, err := larkpushcap.ListPushApps(ctx, "my-plugin") // 发送前按本分组的推送应用预检（如卡片模板是否齐备）

res, err := larkpushcap.SendCard(ctx, larkpushcap.SendCardRequest{
    Group:      "my-plugin",
    Recipients: []larkpushcap.Recipient{{EmployeeNo: "GZ001198"}},
    Build: func(app larkpushcap.PushApp, r larkpushcap.Recipient) (string, error) {
        return larkim.BuildTemplateCard(templateIDOf(app.AppID), "", map[string]any{
            "url": linkFor(app.IdentityAppID),
        })
    },
    BizSource: "my-plugin",
})
```

未启用本插件时返回 `larkpushcap.ErrSenderUnbound`——**调用方必须把它当失败处理**，
不要当成「已发送」。`BizSource` 只用于日志对账，不参与选路。

## 依赖

- `linapro-employee-core`（`plugin.yaml` 的 `dependencies.plugins` 已声明）：提供
  「工号 → 企业级 user_id」寻址。未启用时门面返回「能力未就绪」、HTTP 返回 503。
- `linapro-lark-sdk`：`larkim`（发送 + 429 退避）。配置读取与校验在本插件的
  `backend/internal/larkcfg`。

**启用顺序**：在 `plugin.autoEnable` 中必须排在 `linapro-employee-core` 之后（数组顺序
即启用顺序）。

## 飞书侧前置（人工）

1. 每个调用方分组、每个企业各建一个**消息推送应用**（或让多个分组共用），开通 `im:message:send_as_bot` 并配置可用范围
   （可用范围必须覆盖收件人）；
2. 需要文件推送的推送应用再开通 `im:resource:upload`（或 `im:resource`）——缺少时文件接口的失败项
   `lark_code` 为 `99991672`；
3. 给每个**身份应用**补充「获取用户 user_id」权限并发布版本——否则 `lark_user_id`
   恒为空、推送无法寻址（`status` 端点的 `blank_user_id_rows` 会暴露这一点）；
4. 确认推送应用的机器人名与考勤/登录机器人不同，且不同分组的机器人名可区分（用户侧
   一眼能分辨来源）。

## 已知取舍

- **日志轮转后不可追溯**：本期不落投递记录表，外部系统追问「上周三那条为什么没到」时
  只能查当期日志。若后续需要，可追加投递记录表（DDL 与保留策略另行设计），不影响契约。
- **签名密钥明文入库**：HMAC 验签需要原值，持「查看密钥」权限者可反复查看；数据库备份须按含密钥
  数据管理。本期不做 IP 白名单。
- **每次签名请求查一次库**：有意不加缓存——请求量低，换来停用 / 删除即时生效。
- **文件请求驻留内存**：最大 31 MB，并按推送应用各上传一次。
- **请求体不签名**（调用方要求，文本与文件接口一致）：签名仍覆盖方法、路径、查询串、时间戳与 nonce，
  凭据校验与防重放照常生效，但传输途中被篡改的消息内容、文件或收件人无法被签名发现，必须走 HTTPS。
- **部分失败不是 0**：返回 `LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED`，调用方需按 `data.failed` 中的收件人重试，不要整批重发（否则成功的那几条会重复）。
