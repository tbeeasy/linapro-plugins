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
)

const (
	defaultTenantID     = 1
	defaultIntervalMin  = 5
	defaultUniqueField  = "工号"
	defaultKeySeparator = "-"
)

// 同步形态枚举值。
const (
	ShapeFlat  = "flat"  // 扁平 upsert（缺省），报表一行 = Bitable 一行
	ShapePivot = "pivot" // 转置：「月份为行、指标为列」→「指标为行、月份为列」
)

// ReportSource 标识一个映射使用哪个 Moka API 后端。
type ReportSource string

const (
	SourceHCM     ReportSource = "hcm"     // Basic Auth + RSA 签名（默认）
	SourceRecruit ReportSource = "recruit" // Basic Auth（apiKey），与 HCM 共用同一把 moka apiKey
)

// DerivedColumn 声明一条列派生规则：从一或多个源列按指定 kind 生成目标列。
// kind 从封闭注册表解析（date / split / regex）。
type DerivedColumn struct {
	Kind    string            `json:"kind"`
	Sources []string          `json:"sources"`
	Targets map[string]string `json:"targets"` // 目标列名 → kind 相关参数（如 layout）
	// split/regex 的参数直接内联到 map；date 用 Targets 的键作列名、值作 layout。
	By      string `json:"by,omitempty"`      // split: 分隔符
	Pattern string `json:"pattern,omitempty"` // regex: 正则表达式
}

// SkipRule 声明一条行级过滤规则：把满足条件的报表行在进入规划前丢弃。
// kind 从封闭注册表解析（当前仅 before）；每条规则只作用于声明它的映射。
type SkipRule struct {
	Kind   string `json:"kind"`   // 比较类型，从封闭注册表解析（当前支持 "before"）
	Source string `json:"source"` // 源列名（可为派生列），取该列单元格值参与比较
	Format string `json:"format"` // 源列值与阈值的解析格式，用 gtime 方言（Y=年 m=月 d=日，如 "Y-m"）
	Value  string `json:"value"`  // 阈值字面量，按 Format 解析后与源列值比较（固定值，非相对当前时间）
}

// ReportMapping 把一个 Moka 报表绑定到一张飞书多维表格。
type ReportMapping struct {
	ReportID     int64        `json:"reportId"`
	AppToken     string       `json:"appToken"`
	TableID      string       `json:"tableId"`
	UniqueFields []string     `json:"uniqueFields"`
	KeySeparator string       `json:"keySeparator"`
	Remark       string       `json:"remark"`
	Enable       bool         `json:"enable"`
	Source       ReportSource `json:"source"` // "hcm"（默认）或 "recruit"
	// Shape 声明同步形态："flat"（缺省）或 "pivot"（转置）。
	Shape string `json:"shape,omitempty"`
	// PivotHeaderColumn 仅 shape=pivot 时有效：指定「其行值转为目标列头」的源列名
	// （如源报表日期列 "公共日期"，其行值为 "2026-01"/"2026-02"/…）。
	PivotHeaderColumn string `json:"pivotHeaderColumn,omitempty"`
	// PivotIndexColumn 仅 shape=pivot 时有效且必填：转置后「承载指标名」的输出索引列名，
	// 须与目标 Bitable 的指标标签列同名（如 "招聘漏斗图"）。它与 PivotHeaderColumn 解耦——
	// 源报表读哪一列的行值当列头由 PivotHeaderColumn 决定，指标名写进哪一列去和目标表对齐由
	// 本字段决定；二者常不同名。缺省不回退到 PivotHeaderColumn，未声明时 pivot 映射本轮跳过。
	PivotIndexColumn string `json:"pivotIndexColumn,omitempty"`
	// PersonFieldSources 声明「Bitable 人员字段名 → Moka 报表工号列名」的映射。
	PersonFieldSources map[string]string `json:"personFieldSources,omitempty"`
	// DerivedColumns 列派生规则，在 Plan 前执行。
	DerivedColumns []DerivedColumn `json:"derivedColumns,omitempty"`
	// SkipIf 行过滤规则，在列派生之后、Coalesce 之前执行；任一规则命中即丢弃该行。
	// 仅 shape=flat 生效；未配置或为空数组时不做任何行过滤。
	SkipIf []SkipRule `json:"skipIf,omitempty"`
}

// Config 是单次同步周期解析后的插件配置。
type Config struct {
	// HCM(Basic Auth + RSA)认证凭据:API Key、API Code、企业编码、私钥
	MokaCred hcm.HCMCredential
	// Moka 接口基础地址(为空时使用默认地址)。HCM 与招聘共用同一 Moka 主机。
	MokaBase string

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
		return nil, gerror.New("linapro-moka-report-sync: 宿主机服务不可用")
	}
	hc := services.HostConfig()
	if hc == nil {
		return nil, gerror.New("linapro-moka-report-sync: 宿主机配置能力不可用")
	}

	apiKey, _ := hc.String(ctx, keyMokaAPIKey, "")
	entCode, _ := hc.String(ctx, keyMokaEntCode, "")
	pemKey, _ := hc.String(ctx, keyMokaPrivateKey, "")
	baseURL, _ := hc.String(ctx, keyMokaBaseURL, hcm.DefaultBaseURL)
	larkAppId, _ := hc.String(ctx, keyLarkAppID, "")
	larkAppKey, _ := hc.String(ctx, keyLarkSecret, "")
	tenantID, _ := hc.Int(ctx, keyTenantID, defaultTenantID)
	intervalMin, _ := hc.Int(ctx, keyIntervalMin, defaultIntervalMin)

	if apiKey == "" || entCode == "" || pemKey == "" {
		return nil, gerror.New("linapro-moka-report-sync: Moka HCM 凭据不完整(apiKey/entCode/rsaPrivateKey)")
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
		return nil, gerror.New("linapro-moka-report-sync: 飞书凭据不完整(appId/appSecret)")
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
		MokaBase:   baseURL,
		LarkApp:    larkAppId,
		LarkAppKey: larkAppKey,
		TenantID:   tenantID,
		Interval:   time.Duration(intervalMin) * time.Minute,
		Mappings:   mappings,
	}, nil
}

// applyMappingDefaults 对单条映射做缺省归一：
//   - uniqueFields 空 → ["工号"]
//   - keySeparator 空 → defaultKeySeparator
//   - source 空 → SourceHCM
//   - shape 空 → ShapeFlat（向后兼容）
func applyMappingDefaults(m *ReportMapping) {
	if len(m.UniqueFields) == 0 {
		m.UniqueFields = []string{defaultUniqueField}
	}
	if m.KeySeparator == "" {
		m.KeySeparator = defaultKeySeparator
	}
	if m.Source == "" {
		m.Source = SourceHCM
	}
	if m.Shape == "" {
		m.Shape = ShapeFlat
	}
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
		return nil, gerror.Wrap(err, "linapro-moka-report-sync: 解析报表映射 JSON 失败")
	}
	for i := range mappings {
		applyMappingDefaults(&mappings[i])
	}
	return mappings, nil
}
