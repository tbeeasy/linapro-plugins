// 本文件定义接口请求日志列表与详情共用的响应结构。
package v1

// ApiLogListItem 是列表中的一条请求日志摘要。
//
// 刻意不含请求头、请求体与响应体：列表受 `monitor:apilog:list` 控制，
// 这些载荷需要更强的 `monitor:apilog:query` 权限。
type ApiLogListItem struct {
	Id           int64  `json:"id"           dc:"日志记录 ID" eg:"1"`
	RequestAt    int64  `json:"requestAt"    dc:"请求开始时间（Unix 毫秒时间戳）；表按它做月度分区，打开详情时须原样回传" eg:"1793793677133"`
	RequestId    string `json:"requestId"    dc:"请求 ID，可与业务日志互查" eg:"7f3c2a9b8e1d4f50a1b2c3d4e5f60718"`
	RouteOwner   string `json:"routeOwner"   dc:"命中路由的归属插件 ID" eg:"linapro-eval-360"`
	Method       string `json:"method"       dc:"HTTP 方法" eg:"POST"`
	Path         string `json:"path"         dc:"实际请求路径" eg:"/x/linapro-eval-360/api/v1/eval/emp/submit"`
	RoutePath    string `json:"routePath"    dc:"路由模式，例如 /task/{id}；动态插件等于实际路径" eg:"/x/linapro-eval-360/api/v1/eval/emp/submit"`
	Query        string `json:"query"        dc:"原始查询串，敏感参数已脱敏" eg:"page=1&size=20"`
	ClientIp     string `json:"clientIp"     dc:"客户端 IP" eg:"10.0.0.12"`
	UserAgent    string `json:"userAgent"    dc:"User-Agent 请求头" eg:"Mozilla/5.0"`
	CallerKind   string `json:"callerKind"   dc:"调用方类型：emp=员工，app_key=外部系统；未识别调用方时为空" eg:"emp"`
	CallerId     string `json:"callerId"     dc:"调用方标识：emp 时为 EmpID，app_key 时为 app_key；唯一的机器关联键" eg:"1840"`
	CallerTenant int    `json:"callerTenant" dc:"调用方租户 ID；无租户语义时为 0" eg:"1"`
	CallerName   string `json:"callerName"   dc:"调用方姓名，写入时的快照" eg:"刘鹏"`
	CallerNo     string `json:"callerNo"     dc:"调用方工号，写入时的快照" eg:"GZ000001"`
	StatusCode   int    `json:"statusCode"   dc:"HTTP 响应状态码" eg:"200"`
	CostMs       int    `json:"costMs"       dc:"请求耗时，单位毫秒" eg:"32"`
	ErrorMsg     string `json:"errorMsg"     dc:"请求失败时的错误信息" eg:""`
}

// ApiLogDetailItem 是一条请求日志的完整详情，包含审计载荷。
type ApiLogDetailItem struct {
	ApiLogListItem
	ReqHeaders string `json:"reqHeaders" dc:"请求头 JSON 对象，敏感值已脱敏" eg:"{\"User-Agent\":\"Mozilla/5.0\"}"`
	ReqBody    string `json:"reqBody"    dc:"完整请求体，敏感字段已脱敏" eg:"{\"items\":[]}"`
	RespBody   string `json:"respBody"   dc:"完整响应体，敏感字段已脱敏" eg:"{\"code\":0}"`
}
