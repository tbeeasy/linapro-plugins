// Package config 从宿主机能力加载 linapro-moka-report-sync 的运行时配置。
// Moka 凭据来自静态宿主机配置文件；报表→表格映射来自受治理的 sys_config 行（后端设置）。
package config

import (
	"context"
	"encoding/json"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/plugin/capability"
	"lina-core/pkg/plugin/capability/bizctxcap"
	"lina-core/pkg/plugin/capability/hostconfigcap"

	"lina-plugin-linapro-moka-hcm/backend/hcm"
)

const (
	// linapro-moka-report-sync 插件配置键 —— 仅 Moka HCM 认证凭据。
	// apiCodes 以 map 形式读取：plugin.linapro-moka-report-sync.moka.apiCodes.<interfaceName>
	// 新增一个 Moka 接口只需在 yaml 中新增一项；Load 无需改动。
	keyMokaAPIKey     = "plugin.linapro-moka-report-sync.moka.apiKey"
	keyMokaEntCode    = "plugin.linapro-moka-report-sync.moka.entCode"
	keyMokaPrivateKey = "plugin.linapro-moka-report-sync.moka.rsaPrivateKey"
	keyMokaBaseURL    = "plugin.linapro-moka-report-sync.moka.baseURL"
	keyMokaAPICodes   = "plugin.linapro-moka-report-sync.moka.apiCodes"

	// linapro-moka-report-sync 插件配置键 —— 飞书凭据与同步设置。
	keyLarkAppID      = "plugin.linapro-moka-report-sync.lark.appId"
	keyLarkSecret     = "plugin.linapro-moka-report-sync.lark.appSecret"
	keyTenantID       = "plugin.linapro-moka-report-sync.tenantId"
	keyIntervalMin    = "plugin.linapro-moka-report-sync.intervalMinutes"
	keyReportMappings = "plugin.linapro-moka-report-sync.reportMappings"

	// linapro-moka-recruit 插件配置键 —— 招聘数据源的 OAuth2 凭据。
	keyRecruitClientID     = "plugin.linapro-moka-report-sync.moka.clientId"
	keyRecruitClientSecret = "plugin.linapro-moka-report-sync.moka.clientSecret"
	keyRecruitBaseURL      = "plugin.linapro-moka-report-sync.moka.baseURL"
)

const (
	defaultTenantID    = 1
	defaultIntervalMin = 5
	defaultUniqueField = "工号"
)

// ReportSource 标识一个映射使用哪个 Moka API 后端。
type ReportSource string

const (
	SourceHCM     ReportSource = "hcm"     // Basic Auth + RSA 签名（默认）
	SourceRecruit ReportSource = "recruit" // OAuth2 Bearer 令牌
)

// ReportMapping 把一个 Moka 报表绑定到一张飞书多维表格。
type ReportMapping struct {
	ReportID    int64        `json:"reportId"`
	AppToken    string       `json:"appToken"`
	TableID     string       `json:"tableId"`
	UniqueField string       `json:"uniqueField"`
	Remark      string       `json:"remark"`
	Enable      bool         `json:"enable"`
	Source      ReportSource `json:"source"` // "hcm"（默认）或 "recruit"
}

// Config 是单次同步周期解析后的插件配置。
type Config struct {
	// HCM(Basic Auth + RSA)认证凭据:API Key、API Code、企业编码、私钥
	MokaCred hcm.HCMCredential
	// Moka HCM 接口基础地址(为空时使用默认地址)
	MokaBase string

	// 招聘侧 OAuth2 认证凭据。没有配置招聘类映射时为空
	RecruitClientID     string // 招聘 OAuth2 Client ID
	RecruitClientSecret string // 招聘 OAuth2 Client Secret
	RecruitBase         string // 招聘接口基础地址

	LarkApp    string          // 飞书自建应用 App ID
	LarkAppKey string          // 飞书自建应用 App Secret
	TenantID   int             // 租户 ID(用于加载报表映射配置)
	Interval   time.Duration   // 同步间隔(由 intervalMinutes 配置换算,重启生效)
	Mappings   []ReportMapping // 报表映射列表:每项把一个 Moka 报表同步到一张飞书多维表格
}

// IntervalMinutes 仅读取配置的同步间隔（分钟），未设置或无效时应用默认值。
// 在任务注册阶段使用。
func IntervalMinutes(ctx context.Context, services capability.Services) int {
	if services == nil || services.HostConfig() == nil {
		return defaultIntervalMin
	}
	n, _ := services.HostConfig().Int(ctx, keyIntervalMin, defaultIntervalMin)
	if n <= 0 {
		return defaultIntervalMin
	}
	return n
}

// Load 解析完整配置。缺少必需凭据时返回错误，以便调用方记录日志并跳过本轮同步。
func Load(ctx context.Context, services capability.Services) (*Config, error) {
	if services == nil {
		return nil, gerror.New("moka-report-sync: host services unavailable")
	}
	hc := services.HostConfig()
	if hc == nil {
		return nil, gerror.New("moka-report-sync: host config capability unavailable")
	}

	apiKey, _ := hc.String(ctx, keyMokaAPIKey, "")
	entCode, _ := hc.String(ctx, keyMokaEntCode, "")
	pemKey, _ := hc.String(ctx, keyMokaPrivateKey, "")
	baseURL, _ := hc.String(ctx, keyMokaBaseURL, hcm.DefaultBaseURL)
	larkAppId, _ := hc.String(ctx, keyLarkAppID, "")
	larkAppKey, _ := hc.String(ctx, keyLarkSecret, "")
	tenantID, _ := hc.Int(ctx, keyTenantID, defaultTenantID)
	intervalMin, _ := hc.Int(ctx, keyIntervalMin, defaultIntervalMin)

	// 招聘凭据为可选项；仅当存在招聘类映射时才校验。
	recruitClientID, _ := hc.String(ctx, keyRecruitClientID, "")
	recruitClientSecret, _ := hc.String(ctx, keyRecruitClientSecret, "")
	recruitBase, _ := hc.String(ctx, keyRecruitBaseURL, "")

	if apiKey == "" || entCode == "" || pemKey == "" {
		return nil, gerror.New("moka-report-sync: incomplete Moka HCM credentials (apiKey/entCode/rsaPrivateKey)")
	}

	// apiCodes 以 map 形式配置在 moka.apiCodes 下；每个 key 是一个接口名
	// （如 "reportData"），value 是 Moka 签发的 apiCode。
	// 新增接口只需新增一个 yaml 项 —— Load 无需改动。
	apiCodesVar, _ := hc.Get(ctx, keyMokaAPICodes, nil)
	var apiCodes map[string]string
	if apiCodesVar != nil {
		apiCodes = apiCodesVar.MapStrStr()
	}
	if larkAppId == "" || larkAppKey == "" {
		return nil, gerror.New("moka-report-sync: incomplete lark credentials (appId/appSecret)")
	}
	priv, err := hcm.ParsePrivateKey(pemKey)
	if err != nil {
		return nil, err
	}
	if intervalMin <= 0 {
		intervalMin = defaultIntervalMin
	}
	if tenantID <= 0 {
		tenantID = defaultTenantID
	}

	mappings, err := loadMappings(ctx, hc, tenantID)
	if err != nil {
		return nil, err
	}

	return &Config{
		MokaCred: hcm.HCMCredential{
			APIKey:     apiKey,
			EntCode:    entCode,
			PrivateKey: priv,
			APICodes:   apiCodes,
		},
		MokaBase:            baseURL,
		RecruitClientID:     recruitClientID,
		RecruitClientSecret: recruitClientSecret,
		RecruitBase:         recruitBase,
		LarkApp:             larkAppId,
		LarkAppKey:          larkAppKey,
		TenantID:            tenantID,
		Interval:            time.Duration(intervalMin) * time.Minute,
		Mappings:            mappings,
	}, nil
}

func loadMappings(ctx context.Context, hc hostconfigcap.Service, tenantID int) ([]ReportMapping, error) {
	sc := hc.SysConfig()
	if sc == nil {
		return nil, nil
	}
	tctx := bizctxcap.WithCurrentContext(ctx, bizctxcap.CurrentContext{TenantID: tenantID})
	info, err := sc.Get(tctx, keyReportMappings)
	if err != nil || info == nil {
		return nil, nil
	}
	raw := info.Value
	if raw == "" {
		return nil, nil
	}

	var mappings []ReportMapping
	if err := json.Unmarshal([]byte(raw), &mappings); err != nil {
		return nil, gerror.Wrap(err, "moka-report-sync: decode report mappings JSON")
	}
	for i := range mappings {
		if mappings[i].UniqueField == "" {
			mappings[i].UniqueField = defaultUniqueField
		}
		if mappings[i].Source == "" {
			mappings[i].Source = SourceHCM
		}
	}
	return mappings, nil
}
