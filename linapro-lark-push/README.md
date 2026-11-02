# linapro-lark-push

Feishu (Lark) message push capability plugin (text, files / images, plus in-process
interactive cards; recipients are people or group chats). Its only page is the
signing-credential admin page under **System Settings**.

It answers one question: **how does any plugin or external system send a Feishu
message to a person or a group chat?** For a person the caller supplies an employee number; the plugin
looks up which Feishu companies that person has an identity in and sends one
message through each company's *push app* bot from **the caller's own group**
(never the sign-in / attendance bot). Different callers use different groups, so
recipients see different bot names (e.g. "360 Assessment" vs "Attendance").

## Scope

Does:

- In-process facade `backend/cap/larkpushcap`: other plugins request delivery with
  "employee no / user_id + text", or deliver interactive cards with
  "employee no / user_id + card builder" (e.g. `linapro-eval-360`'s assessment
  template card).
- External HTTP endpoints `POST /x/linapro-lark-push/open/v1/message/text` and
  `POST /x/linapro-lark-push/open/v1/message/file` (one-step upload and push) with
  plugin-owned HMAC-SHA256 signature auth.
- Group chats: for the external API the target chat is configured on the signing
  credential in the admin page (callers cannot pass a `chat_id`); the in-process
  facade can address a `chat_id` directly (text, files and cards).
- Admin page **System Settings → Lark Push Credentials** to manage signing
  credentials.
- Self-check endpoint `GET /x/linapro-lark-push/open/v1/status` for deploy
  verification and alerting.
- Failed items in the response (recipient + Feishu error code / message / request ID);
  per-recipient × per-push-app details go to structured logs.

Does not (this iteration):

- Cards, rich text or mentions over external HTTP — the external endpoints send
  text and files; cards are in-process only.
- Delivery-record persistence, retry queues, exactly-once semantics. Failures are
  logged and returned in the details; the caller retries.
- Host in-app messaging (no `notifycap`, no `usermsg`).
- Choosing a company on the caller's behalf — fan-out is automatic, and duplicated
  delivery to a person present in two companies is expected.

## Three hard constraints

1. **Delivery credentials come only from the request group's `push_groups.<group>`.**
   The send loop iterates only that group's push apps; `identity_app_id` is merely a
   string used to query `lark_identity` — this plugin cannot read identity-app
   secrets and does not need them, so they never enter the send path. This makes
   "alerts must not come from the attendance bot" a structural guarantee.
2. **The group is required and never falls back.** In-process callers must pass
   `Group` (their own plugin ID); for external HTTP the group is bound to the
   signing credential. An empty group is a parameter error; an unconfigured group
   returns "no push app in this group" — it **never** switches to another group or
   to all push apps.
3. **One request may send several messages.** A person with identities in two
   companies receives two (different bots, so from the user's perspective two
   systems each notified them once). The result details list company, status and
   Feishu message id for each.

## Configuration

### Feishu push credentials (`plugin.linapro-lark-push.push_groups`)

Push-app credentials are this plugin's **private** configuration, grouped by
caller: the key is the group name, the value is a push-app array (one entry per
company that receives messages). Group name = the caller's plugin ID; groups used
by external HTTP callers are named freely and bound through signing credentials.
Sign-in / directory-sync apps belong to `linapro-employee-core`
(`plugin.linapro-employee-core.lark_apps`); this plugin never reads their secrets.

```yaml
plugin:
  linapro-lark-push:
    push_groups:
      linapro-eval-360:            # group name = caller plugin ID
        - app_id: cli_push_a       # a dedicated push app — never reuse the sign-in app
          secret_key: "xxx"
          name: "360 Assessment (Horgos)"
          identity_app_id: cli_xxx # the same company's app_id in employee-core lark_apps
      linapro-attend-poc:          # exception: attendance sends via the sign-in app itself (the attendance bot)
        - app_id: cli_xxx
          secret_key: "same as cli_xxx in lark_apps"
          name: "Attendance (Horgos)"
          identity_app_id: cli_xxx
      ops-alert:                   # group for external HTTP, named freely
        - app_id: cli_push_d
          secret_key: "zzz"
          name: "Ops alert (Horgos)"
          identity_app_id: cli_xxx
```

Validation (each send checks only the request group; the `status` endpoint checks
all groups):

- within a group, `app_id` / `secret_key` / `identity_app_id` non-empty;
- within a group, `app_id` unique;
- within a group, the same `identity_app_id` must not be referenced by two push
  apps (a person would receive duplicates);
- the same `app_id` **may** appear in several groups (callers sharing one bot);
- an invalid group only disables that group; other groups keep delivering. Only a
  `push_groups` value whose structure cannot be parsed disables everything.

Feishu apps needed = groups × companies (unless groups share apps).

`identity_app_id` points at an app declared in employee-core's config, which this
plugin cannot see, so a **dangling reference cannot be caught at config time** — it
shows up as `addressable_employees == 0` for that push app on `status`.

### Signing credentials (admin page "System Settings → Lark Push Credentials")

The external HTTP API is authenticated with **HMAC-SHA256 signatures**. Credentials
are stored in this plugin's table `plugin_linapro_lark_push_api_key` and maintained
from the admin page **System Settings → Lark Push Credentials** (there is no config
key). Each credential is `{name, app_key, secret, group, chat_id, chat push app, status, remark}`:

- `app_key` is a **public identifier** — it travels in the `X-App-Key` header and
  appears in logs. 1–64 characters of letters, digits, `_`, `-`, `.`; unique among
  non-deleted credentials (a deleted `app_key` can be created again).
- `secret` is the signing key. It **never travels over the network**: the caller
  uses it locally to compute the signature, and the server uses it to recompute
  and compare. It is never logged. It must be 16–128 visible ASCII characters
  and is required on create: the admin page pre-fills a 64-character alphanumeric
  random value in the browser when the create form opens, which can be edited or
  regenerated; an empty value cannot be submitted, and the server also rejects it
  rather than generating one. Leaving it empty on edit keeps the current value. The list and detail
  APIs never return it — viewing it requires the separate "View Secret" permission.
- `group` binds the credential to one push group configured in `push_groups`; the
  delivery group comes from the matched credential, and the request body cannot
  choose a group (a `group` field is rejected with `LINAPRO_LARK_PUSH_INVALID_PARAM`), so a credential holder
  cannot impersonate another group's bot.
- `chat_id` (optional, starts with `oc_`): once set, **every** text / file push signed
  with this credential is also delivered to that chat. Callers cannot choose a chat in
  the request (a `chats` field is rejected as an invalid parameter), so a credential holder cannot
  send to arbitrary chats. Use one credential per chat.
- Chat push app (optional): which push app (bot) of the group posts to the chat. It
  may be left empty when the group has a single push app and is required otherwise;
  it must belong to the group. That bot must already be in the chat, otherwise the
  chat delivery is `failed`.
- `status` uses the host dictionary `sys_normal_disable` (`1` normal, `0` disabled).

The signature middleware **queries the table on every request** (one indexed
lookup, no cache), so disabling, deleting or editing a credential takes effect on
the very next request. Rotate by creating a new credential for the caller, waiting
until it has switched, then disabling or deleting the old one.

| Permission | Meaning |
|---|---|
| `linapro-lark-push:api-key:list` | open the page, list / view credentials, read push groups |
| `linapro-lark-push:api-key:add` | create credentials |
| `linapro-lark-push:api-key:edit` | edit and enable / disable credentials |
| `linapro-lark-push:api-key:remove` | delete credentials (soft delete) |
| `linapro-lark-push:api-key:secret` | view the `secret` in plain text |

## Signed requests

### Request headers

| Header | Meaning |
|---|---|
| `X-App-Key` | public credential identifier |
| `X-Timestamp` | Unix time in **seconds**; must be within **±300 s** of the server clock |
| `X-Nonce` | random string, **16–64 characters**; must be unique per request |
| `X-Signature` | lowercase hex HMAC-SHA256 of the string to sign |

### String to sign

```text
METHOD + "\n" + PATH + "\n" + CANONICAL_QUERY + "\n" + TIMESTAMP + "\n" + NONCE + "\n" + hex(SHA256(""))
```

- `METHOD` — uppercase HTTP method.
- `PATH` — request path only, without the query string, e.g.
  `/x/linapro-lark-push/open/v1/message/text`.
- `CANONICAL_QUERY` — query parameters sorted by name, then by value, each
  rendered as `urlencode(name)=urlencode(value)` (space → `+`) and joined with
  `&`. Empty string when there are no parameters.
- Last line — **no endpoint signs its request body**; the last line is always the
  SHA-256 of the empty string:
  `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`. The line is kept
  only so the string-to-sign keeps its existing shape; callers never read or hash the
  body to sign (the file endpoint does not need the whole file in memory).

Because the signature covers method, path, query, timestamp and nonce, tampering with
any of them invalidates it. **The request body (message text, file, recipients) is
not protected by the signature — use HTTPS in production.** A captured request can be replayed only until its
timestamp leaves the window, and the nonce blocks even that: **the same nonce for
the same `app_key` succeeds at most once**.

### Go example

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

	// The body is not signed: the last line is the SHA-256 of "".
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
	body := []byte(`{"recipients":[{"employee_no":"GZ001198"}],"text":"Alert: line 3 turbine temperature above threshold","biz_ref":"alert-20261021-001"}`)

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

### PHP example

A complete runnable demo (status, text, file, group chats; PHP >= 7.4 with the curl
extension) lives in
[`hack/examples/php/lark-push-demo.php`](hack/examples/php/lark-push-demo.php). Minimal
text snippet:

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
    $method   = 'POST';
    $path     = '/x/linapro-lark-push/open/v1/message/text';
    $body     = json_encode($payload, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
    $timestamp = (string) time();
    // 32 个十六进制字符，满足 16~64 的长度要求；每条请求都必须不同。
    $nonce    = bin2hex(random_bytes(16));

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
    // Unified envelope: code=0 means all delivered; errorCode=LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED
    // means data.failed lists the failed recipients (retry only those).
    echo $result['status'], PHP_EOL;
    print_r($result['body']);
} catch (Throwable $e) {
    fwrite(STDERR, $e->getMessage() . PHP_EOL);
    exit(1);
}
```

> If the endpoint ever takes query parameters, sort them by name and then by
> value, `urlencode()` each name and value (PHP's `urlencode()` matches Go's
> `url.QueryEscape()`, including space → `+`), and join with `&` — that becomes
> `CANONICAL_QUERY`. `http_build_query()` alone is not sufficient because it does
> not sort.

### curl / shell example

```bash
HOST='https://<host>'
APP_KEY='ops-alert-prod'
SECRET='<secret>'
PATH_ONLY='/x/linapro-lark-push/open/v1/message/text'
BODY='{"recipients":[{"employee_no":"GZ001198"}],"text":"Alert: line 3 turbine temperature above threshold"}'

TS=$(date +%s)
NONCE=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
EMPTY_BODY_HASH='e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855' # body is not signed; fixed empty-string digest
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

`printf '%s'` (not `echo`) matters when signing: `echo` appends a newline, which
would change the string to sign.

### Response envelope and error codes

Every external endpoint responds with the same unified envelope as the admin APIs (FB-12):

```json
{"code": 0, "message": "OK", "data": {...}}
{"code": 61, "message": "Signature mismatch", "data": null,
 "errorCode": "LINAPRO_LARK_PUSH_SIGNATURE_MISMATCH", "messageKey": "error.linapro.lark.push.signature.mismatch"}
```

- Success or failure: check `code` — `0` is success, anything else is a failure;
- The specific reason: check `errorCode` (a stable string); `message` is for display only and may change;
- HTTP status has only three exceptions: **413** for an oversized body, **500** for server-side push
  configuration problems, **503** when a server capability is unavailable; everything else (invalid
  parameters, signature errors, partial delivery failures) returns **200**.

| HTTP | `errorCode` | cause |
|---|---|---|
| 200 | `LINAPRO_LARK_PUSH_INVALID_PARAM` | invalid parameter: bad JSON or unknown field, empty / too long text, no recipient, invalid file form |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_MISSING` | a signature header is missing, or `X-Nonce` is not 16–64 chars |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_UNKNOWN_APP_KEY` | `X-App-Key` does not match an enabled, non-deleted credential |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_TIMESTAMP_INVALID` | timestamp unparseable or outside ±300 s — check the caller clock |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_MISMATCH` | signature does not match — path, query, method, timestamp or nonce differ, or the last line still hashes the body |
| 200 | `LINAPRO_LARK_PUSH_SIGNATURE_NONCE_REPLAYED` | the same `app_key` already used this `nonce` |
| 200 | `LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED` | some recipients failed; details in `data.failed` (see below) |
| 413 | `LINAPRO_LARK_PUSH_BODY_TOO_LARGE` | body exceeds the limit: 256 KB for text, 31 MB for file |
| 500 | `LINAPRO_LARK_PUSH_NO_PUSH_APP` / `LINAPRO_LARK_PUSH_CONFIG_INVALID` | the credential's group has no usable push app, or the push config is invalid |
| 503 | `LINAPRO_LARK_PUSH_CREDENTIAL_STORE_UNAVAILABLE` | the credential table cannot be queried — fail-closed, nothing is sent |
| 503 | `LINAPRO_LARK_PUSH_NONCE_STORE_UNAVAILABLE` | replay-protection cache unavailable — fail-closed, never "skip the check" |
| 503 | `LINAPRO_LARK_PUSH_SENDER_UNBOUND` / `LINAPRO_LARK_PUSH_EMPCAP_UNBOUND` | push or employee capability not ready |

When the plugin is not enabled the host returns 404 directly (not JSON); that is host behaviour.

Signature verification happens **before** the nonce is recorded, so a request
with a bad signature never consumes a nonce — an attacker cannot burn a
legitimate in-flight nonce. Clocks must stay within ±300 s of the server (use
NTP).

## External API

```bash
# See "Go example" / "PHP example" / "curl / shell example" above for a signed request.
```

The delivery result is in the envelope's `data` and only carries the outcome and the
failed items; per-delivery details (push app, message_id, error text) are written to
server logs only (search by `request_id` / `biz_ref`).

All succeeded → `code` is `0`:

```json
{"code": 0, "message": "OK", "data": {"all_succeeded": true}}
```

Any failure → HTTP **200**, non-zero `code`, `errorCode` is
`LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED`; `data.failed` lists the failed items so the
caller retries only those recipients:

```json
{
  "code": 60,
  "message": "3 recipient(s) failed to receive the message",
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

| field | meaning |
|---|---|
| `employee_no` / `user_id` / `chat_id` | the failed recipient, in the same addressing form as the request |
| `lark_code` | Feishu error code; `0` for non-Feishu reasons |
| `lark_msg` | Feishu error message; for non-Feishu reasons one of the values below |
| `lark_req_id` | Feishu request ID for Feishu support; empty for non-Feishu reasons |

Non-Feishu reasons (`lark_code` is `0`):

| `lark_msg` | meaning | action |
|---|---|---|
| `no_identity` | the person has no own identity in **any** company (missing in only some companies is normal and not a failure); a mistyped or nonexistent number also lands here | check the number or the identity sync |
| `user_id_missing` | own row exists but `lark_user_id` is blank | usually the identity app lacks the "get user user_id" permission, or the row awaits the next sync |
| `collision` | the employee number hits several own identities in a company | data problem; refused to avoid messaging the wrong person |
| `failed` | no Feishu response (e.g. network failure) | safe to retry |

When a person has identities in several companies, each company's failure is listed
separately (the same employee number may appear more than once); retrying that number
sends to all of their companies again, duplicating the one that already succeeded.

Group chats (text and file endpoints): **the request never names a chat**. When the
credential has a `chat_id` configured in the admin page, the server appends that chat
to `recipients` and delivers it once through the credential's chat push app (chats
are not fanned out per company). `recipients` may then be omitted (chat only); if
the credential has no chat configured, `recipients` is required and an empty list
returns `LINAPRO_LARK_PUSH_INVALID_PARAM`.

```json
{
  "text": "Alert: line 3 turbine temperature above threshold"
}
```

Failed chat items carry `chat_id` instead of `employee_no` / `user_id`.

### File push (`POST /x/linapro-lark-push/open/v1/message/file`)

One step: upload and push. The body is `multipart/form-data` with exactly these
fields (any other field is rejected as an invalid parameter):

| Field | Required | Meaning |
|---|---|---|
| `file` | yes | the file, at most 30 MB |
| `recipients` | depends | JSON array string, same elements as the text API; may be omitted when the credential has a chat configured (chat only) |
| `biz_ref` | no | business reference for log reconciliation |

Images (JPEG / PNG / GIF / WEBP / BMP / ICO, detected by content, at most 10 MB) are
sent as image messages; everything else is sent as a file message with `file_type`
derived from the extension (`pdf`, `doc`, `xls`, `ppt`, `mp4`, `opus`, otherwise
`stream`). Feishu file keys belong to the uploading app, so the file is uploaded
once per push app that has at least one addressable delivery; an upload failure
only fails that app's deliveries. The response has the same shape as the text API.
The body limit for this endpoint is 31 MB; the host `sys.upload.maxSize` must not be
lower than 30 MB.

Signed exactly like the text endpoint (the body is not signed), so the multipart
body can be built by the HTTP client as usual. See `larkPushSendFile` in
[`hack/examples/php/lark-push-demo.php`](hack/examples/php/lark-push-demo.php) for PHP;
in Go (reusing `sign` from the Go example):

```go
var body bytes.Buffer
w := multipart.NewWriter(&body)
_ = w.WriteField("recipients", `[{"employee_no":"GZ001198"}]`) // the chat comes from the credential
part, _ := w.CreateFormFile("file", "report.pdf")
_, _ = part.Write(fileBytes)
_ = w.Close()

req, err := sign(http.MethodPost, "https://<host>/x/linapro-lark-push/open/v1/message/file",
    appKey, secret, body.Bytes())
if err != nil {
    panic(err)
}
req.Header.Set("Content-Type", w.FormDataContentType()) // overrides application/json
```

Self-check:

```bash
# GET has an empty body; sign it the same way (see the examples above).
```

The `status` endpoint is signed exactly like `message/text`: same headers, same
string-to-sign.

Reports per "group × push app" (each entry carries `group`): credential
completeness, `identity_app_id` validity, addressable headcount, blank-`user_id`
rows and collision count; config issues carry the group path (e.g.
`push_groups.linapro-eval-360[0].secret_key`). **Deploy
acceptance** requires this endpoint green, one real message per company, and the
sender bot name matching the push app.

## In-process usage

```go
import "lina-plugin-linapro-lark-push/backend/cap/larkpushcap"

res, err := larkpushcap.SendText(ctx, larkpushcap.SendTextRequest{
    Group:      "my-plugin", // required: your own plugin ID; only push_groups.my-plugin is used
    Recipients: []larkpushcap.Recipient{{EmployeeNo: "GZ001198"}},
    Text:       "Alert: line 3 turbine temperature above threshold",
    BizSource:  "my-plugin",
    BizRef:     "alert-001",
})
```

In-process callers (plugin code in the same process) may address a chat directly: a
chat is a `Recipient` with `ChatID` (and `PushAppID` when the group has several push
apps); files go through `SendFile`. This differs from the external API, where the
chat can only come from the credential configuration.

```go
res, err := larkpushcap.SendFile(ctx, larkpushcap.SendFileRequest{
    Group: "my-plugin",
    Recipients: []larkpushcap.Recipient{
        {EmployeeNo: "GZ001198"},
        {ChatID: "oc_xxx", PushAppID: "cli_push_a"},
    },
    FileName:  "report.pdf",
    Content:   pdfBytes,
    BizSource: "my-plugin",
})
```

Interactive cards (in-process only): the caller builds the card content per
"push app × recipient" — a Feishu card template must be granted to the push app
that sends it and cannot be shared across enterprises, and a link that should log
the employee in silently per enterprise must carry `PushApp.IdentityAppID` (the
employee side exchanges codes with the identity app, not the push app). A builder
error marks only that delivery as `failed`; the rest of the batch continues.

```go
apps, err := larkpushcap.ListPushApps(ctx, "my-plugin") // precheck this group's push apps (e.g. templates)

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

When this plugin is disabled the call returns `larkpushcap.ErrSenderUnbound` — callers
**must** treat that as a failure, never as "already sent". `BizSource` is for log
reconciliation only and never affects routing.

## Dependencies

- `linapro-employee-core` (declared in `plugin.yaml` `dependencies.plugins`):
  provides the employee-number → company-level `user_id` lookup. When it is not
  enabled the facade reports "not ready" and the HTTP endpoint returns 503 with `LINAPRO_LARK_PUSH_EMPCAP_UNBOUND`.
- `linapro-lark-sdk`: `larkim` (send + 429 backoff). Config loading and validation
  live in this plugin's `backend/internal/larkcfg`.

**Enable order**: in `plugin.autoEnable` this plugin must come after
`linapro-employee-core` (array order is enable order).

## Feishu-side prerequisites (manual)

1. Create a **message-push app** per caller group and company (or let groups share
   one), grant `im:message:send_as_bot` and
   set the availability scope to cover recipients.
2. Push apps used for file push also need `im:resource:upload` (or `im:resource`);
   without it file deliveries fail with `lark_code` `99991672`.
3. Grant each **identity app** the "get user user_id" permission and publish a new
   version — otherwise `lark_user_id` stays empty and nothing is addressable (the
   `status` endpoint surfaces this as `blank_user_id_rows`).
4. Make sure the push bot's display name differs from the attendance / sign-in bot,
   and that bots of different groups are distinguishable.

## Known trade-offs

- **No audit trail after log rotation**: delivery records are not persisted, so
  "why didn't last Wednesday's message arrive" is only answerable during the log
  retention window. A delivery table can be added later without changing the
  contract.
- **Signing secrets are stored in plain text in the database** (HMAC verification
  needs the original value) and can be viewed repeatedly by holders of the "View
  Secret" permission; treat database backups accordingly. No IP allow-list this
  iteration.
- **One database lookup per signed request**: no cache, by design — request volume
  is low, and disabling or deleting a credential takes effect immediately.
- **File requests are held in memory** (up to 31 MB) and uploaded once per push app.
- **Request bodies are not signed** (caller requirement, same for text and file).
  Method, path, query, timestamp and nonce are still signed, so credential checks and
  replay protection still apply, but message text, files or recipients altered in
  transit cannot be detected by the signature — HTTPS is mandatory.
- **Partial failure is not code 0**: it returns `LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED`; callers must
  retry the recipients listed in `data.failed`, not resend the whole batch (that would duplicate the
  successful ones).
