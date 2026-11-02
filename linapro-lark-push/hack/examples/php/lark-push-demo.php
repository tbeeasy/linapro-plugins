<?php
/**
 * linapro-lark-push 对外接口 PHP 调用示例（PHP >= 7.4，需 curl 扩展）。
 *
 * 覆盖三个接口：
 *   GET  /x/linapro-lark-push/open/v1/status        自检，不发送任何消息
 *   POST /x/linapro-lark-push/open/v1/message/text  文本推送（JSON）
 *   POST /x/linapro-lark-push/open/v1/message/file  文件推送（multipart）
 *
 * 待签串（换行分隔）：
 *   METHOD \n PATH \n 规范化查询串 \n TIMESTAMP \n NONCE \n e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
 * 所有接口的请求体都不参与签名，最后一行恒为空串的 SHA-256（上面这个固定值）。
 * 请求体不受签名保护，生产环境必须走 HTTPS。
 *
 * 用法（凭据在后台「系统设置 → 飞书推送凭据」创建）：
 *   export LARK_PUSH_BASE_URL=https://<host>
 *   export LARK_PUSH_APP_KEY=<app_key>
 *   export LARK_PUSH_SECRET=<secret>
 *   export LARK_PUSH_EMPLOYEE_NOS=GZ001198,GZ001199   # 可选：个人收件人（工号，逗号分隔）
 *
 * 群聊：不在请求里传。在后台为该凭据配置群聊 chat_id 后，每次推送都会同时发到这个群；
 * 凭据配置了群聊时 LARK_PUSH_EMPLOYEE_NOS 可以不设（只发群），否则必须设置。
 *
 *   php lark-push-demo.php status
 *   php lark-push-demo.php text "预警：产线 3 号机组温度超过阈值"
 *   php lark-push-demo.php file ./月报.pdf
 */
declare(strict_types=1);

const LARK_PUSH_PATH_PREFIX = '/x/linapro-lark-push/open/v1';

/**
 * 规范化查询串：按参数名、再按值排序，逐项 urlencode(name)=urlencode(value)，用 & 连接。
 * PHP 的 urlencode() 与服务端 Go url.QueryEscape() 一致（空格为 +）。http_build_query() 不排序，不能直接用。
 */
function larkPushCanonicalQuery(array $query): string
{
    $pairs = [];
    foreach ($query as $name => $values) {
        foreach ((array) $values as $value) {
            $pairs[] = urlencode((string) $name) . '=' . urlencode((string) $value);
        }
    }
    sort($pairs, SORT_STRING);
    return implode('&', $pairs);
}

/** 待签串末行：空串的 SHA-256。请求体不参与签名，该行固定不变。 */
const LARK_PUSH_EMPTY_BODY_DIGEST = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855';

/** 计算签名并返回需要附加的请求头（签名只覆盖方法、路径、查询串、时间戳与 nonce）。 */
function larkPushSignHeaders(
    string $method,
    string $path,
    array $query,
    string $appKey,
    string $secret
): array {
    $timestamp = (string) time();
    // 32 个十六进制字符，满足 16~64 的长度要求；每条请求都必须不同（同一 nonce 只能成功一次）。
    $nonce = bin2hex(random_bytes(16));

    $stringToSign = implode("\n", [
        strtoupper($method),
        $path,
        larkPushCanonicalQuery($query),
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

/**
 * 发送请求并返回 ['status' => HTTP 状态码, 'body' => 解码后的响应]。
 *
 * @param string|array|null $postFields JSON 字符串、multipart 字段数组（含 CURLFile）或 null（GET）
 */
function larkPushDo(string $method, string $url, array $headers, $postFields, int $timeoutSeconds): array
{
    $ch = curl_init($url);
    $options = [
        CURLOPT_CUSTOMREQUEST  => $method,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT        => $timeoutSeconds,
        // 关闭 curl 对大请求体自动发送的 Expect: 100-continue，避免部分代理多等一轮。
        CURLOPT_HTTPHEADER     => array_merge($headers, ['Expect:']),
    ];
    if ($postFields !== null) {
        $options[CURLOPT_POSTFIELDS] = $postFields;
    }
    curl_setopt_array($ch, $options);

    $responseBody = curl_exec($ch);
    if ($responseBody === false) {
        $error = curl_error($ch);
        curl_close($ch);
        throw new RuntimeException('请求失败: ' . $error);
    }
    $status = (int) curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
    curl_close($ch);

    $decoded = json_decode((string) $responseBody, true);
    return ['status' => $status, 'body' => $decoded ?? $responseBody];
}

/** 自检：返回各推送应用凭据与身份数据的状态，不发送任何消息。 */
function larkPushStatus(string $baseUrl, string $appKey, string $secret): array
{
    $path = LARK_PUSH_PATH_PREFIX . '/status';
    $headers = larkPushSignHeaders('GET', $path, [], $appKey, $secret);
    return larkPushDo('GET', $baseUrl . $path, $headers, null, 15);
}

/**
 * 文本推送。$recipients 形如 [['employee_no' => 'GZ001198']]；凭据在后台配置了群聊时可为空数组（只发群）。
 * 请求体不参与签名。
 */
function larkPushSendText(
    string $baseUrl,
    string $appKey,
    string $secret,
    array $recipients,
    string $text,
    string $bizRef = ''
): array {
    $path = LARK_PUSH_PATH_PREFIX . '/message/text';
    $payload = ['text' => $text];
    if ($recipients) {
        $payload['recipients'] = $recipients;
    }
    if ($bizRef !== '') {
        $payload['biz_ref'] = $bizRef;
    }
    $body = json_encode($payload, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR);

    $headers = larkPushSignHeaders('POST', $path, [], $appKey, $secret);
    $headers[] = 'Content-Type: application/json';
    return larkPushDo('POST', $baseUrl . $path, $headers, $body, 15);
}

/**
 * 文件推送（一步式：上传即推送）。图片（≤10 MB）按图片消息发送，其余文件按文件消息发送（≤30 MB）。
 * 请求体不参与签名，multipart 交给 curl 组装即可。收件人规则同文本推送。
 */
function larkPushSendFile(
    string $baseUrl,
    string $appKey,
    string $secret,
    array $recipients,
    string $filePath,
    string $bizRef = ''
): array {
    if (!is_file($filePath)) {
        throw new InvalidArgumentException('文件不存在: ' . $filePath);
    }
    $path = LARK_PUSH_PATH_PREFIX . '/message/file';

    // recipients 是 JSON 数组字符串；只允许 file / recipients / biz_ref 三个字段。
    $fields = ['file' => new CURLFile($filePath, 'application/octet-stream', basename($filePath))];
    if ($recipients) {
        $fields['recipients'] = json_encode($recipients, JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR);
    }
    if ($bizRef !== '') {
        $fields['biz_ref'] = $bizRef;
    }

    // 不要手动设置 Content-Type：curl 收到数组时会自动生成带 boundary 的 multipart/form-data。
    $headers = larkPushSignHeaders('POST', $path, [], $appKey, $secret);
    // 30 MB 文件上传给飞书需要时间，超时放宽。
    return larkPushDo('POST', $baseUrl . $path, $headers, $fields, 120);
}

/** 读取逗号分隔的环境变量，去空白、去空项。 */
function envList(string $name): array
{
    $raw = (string) getenv($name);
    return array_values(array_filter(array_map('trim', explode(',', $raw)), 'strlen'));
}

/** 读取必填环境变量。 */
function envRequired(string $name): string
{
    $value = trim((string) getenv($name));
    if ($value === '') {
        throw new InvalidArgumentException('缺少环境变量 ' . $name);
    }
    return $value;
}

function main(array $argv): int
{
    $command = $argv[1] ?? '';
    $baseUrl = rtrim(envRequired('LARK_PUSH_BASE_URL'), '/');
    $appKey = envRequired('LARK_PUSH_APP_KEY');
    $secret = envRequired('LARK_PUSH_SECRET');

    $recipients = array_map(static function (string $no): array {
        return ['employee_no' => $no];
    }, envList('LARK_PUSH_EMPLOYEE_NOS'));

    $bizRef = 'php-demo-' . date('YmdHis');
    switch ($command) {
        case 'status':
            $result = larkPushStatus($baseUrl, $appKey, $secret);
            break;
        case 'text':
            $result = larkPushSendText($baseUrl, $appKey, $secret, $recipients, $argv[2] ?? 'PHP 示例消息', $bizRef);
            break;
        case 'file':
            $result = larkPushSendFile($baseUrl, $appKey, $secret, $recipients, $argv[2] ?? '', $bizRef);
            break;
        default:
            fwrite(STDERR, "用法: php lark-push-demo.php status | text <内容> | file <文件路径>\n");
            return 2;
    }

    // 响应为统一结构 {code, message, data, errorCode}（与后台接口一致）：code=0 全部成功；
    // errorCode=LINAPRO_LARK_PUSH_SEND_PARTIAL_FAILED 时 data.failed 列出失败的收件人与
    // lark_code / lark_msg / lark_req_id（只重试这些收件人）；参数错误、签名问题同样是 HTTP 200，
    // 按 errorCode 区分。HTTP 状态码只有 413（请求体超限）、500（服务端配置问题）、503（能力不可用）三种例外。
    echo 'HTTP ', $result['status'], PHP_EOL;
    echo json_encode($result['body'], JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_PRETTY_PRINT), PHP_EOL;
    $body = $result['body'];
    return is_array($body) && ($body['code'] ?? null) === 0 ? 0 : 1;
}

try {
    exit(main($argv));
} catch (Throwable $e) {
    fwrite(STDERR, $e->getMessage() . PHP_EOL);
    exit(1);
}
