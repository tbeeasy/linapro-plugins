// empcap_facade.go 把拥有方插件的服务和 token 密钥绑定到公开接缝。
// 员工服务和密钥提供方在启动时由拥有方绑定一次；消费方在请求时懒解析，
// 因此插件 init() 顺序无关紧要 —— 在拥有方绑定之前构造的中间件
// 在第一个真实请求（发生在启动装配完成之后）时依然可用。

package empcap

import (
	"context"
	"sync"
)

// SecretProvider 返回当前上下文的员工 JWT 签名密钥。它由拥有方插件绑定，
// 来源于宿主机配置，必须返回非空密钥或错误（按失败关闭处理，绝不使用空密钥）。
type SecretProvider func(ctx context.Context) (string, error)

var (
	bindMu        sync.RWMutex
	boundService  Service
	boundSecretFn SecretProvider
)

// BindEmployeeService 装配拥有方插件的服务实现。传入 nil 会清空绑定。
func BindEmployeeService(svc Service) {
	bindMu.Lock()
	defer bindMu.Unlock()
	boundService = svc
}

// BoundEmployeeService 返回绑定的服务，未绑定时返回 nil。
func BoundEmployeeService() Service {
	bindMu.RLock()
	defer bindMu.RUnlock()
	return boundService
}

// LarkOpenIDResolverByEmployeeNos 构造一个「多个工号 → 去重后的全部 open_id」解析闭包，
// 供飞书写入侧的消费方解析按工号表达的人员身份（如 recruit-pipeline 写「简历归属者」/多面试官
// 人员列、report-sync 的逗号拼接人员列：工号先按出现顺序拼接，再交本闭包解析为同一多人字段
// 要写入的 open_id 集合）。它在构造时向绑定的员工服务批量拉取一次映射
// （MapLarkOpenIDsByEmployeeNo，按写表格的 larkAppID 作用域过滤），返回的闭包按内存查找，
// 避免逐人查库 N+1；消费方每周期调用一次、复用返回的闭包即可。解析结果由消费方经 larkbitable
// 的 Persons 旁路交给 SDK 序列化写入。
//
// 身份解析（查目录、lark_app 作用域过滤、多工号合并与跨工号去重）集中在数据 owner 侧完成，
// 消费方只做输入格式归一（如拆分逗号拼接值），不再各自实现合并/去重。它同样在构造时批量
// 拉取一次映射，闭包按内存查找、无 N+1。
//
// 返回的 id 为该 larkAppID 作用域下的 open_id（own + assoc、双租户同人多个 open_id 全返回）。
// open_id 按应用作用域签发，写侧应以 user_id_type=open_id 识别人员字段；工号解析到的行仍依赖
// 姓名 JOIN 回填的 lark_identity（未对上则该工号返回空、跳过该列）。
//
// 合并规则：按输入工号的顺序逐个取该工号的 open_id，跨工号按出现顺序去重（同一 HR 命中多个
// open_id 或重复工号都不会重复写入）；空工号段与未命中的工号直接跳过。全部未命中时返回空
// 集合（含 nil），消费方据此跳过该人员列。
//
// 降级语义：员工服务未绑定返回 (nil, nil)，批量查询失败返回 (nil, err)，消费方据此跳过
// 人员列、不阻断同步。
func LarkOpenIDResolverByEmployeeNos(ctx context.Context, larkAppID string) (func(employeeNos []string) []string, error) {
	svc := BoundEmployeeService()
	if svc == nil {
		return nil, nil
	}
	empNoToIDs, err := svc.MapLarkOpenIDsByEmployeeNo(ctx, larkAppID)
	if err != nil {
		return nil, err
	}
	return func(employeeNos []string) []string {
		// 跨工号去重：同一 open_id 可能经不同工号或同一工号的多身份重复命中。
		seen := make(map[string]struct{}, len(employeeNos))
		var ids []string
		for _, no := range employeeNos {
			if no == "" {
				continue
			}
			for _, id := range empNoToIDs[no] {
				if id == "" {
					continue
				}
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
		return ids
	}, nil
}

// LarkUserIDResolverByApp 构造「按身份应用取 user_id 索引」的便捷闭包，与
// LarkOpenIDResolverByEmployeeNos 对齐，供 linapro-lark-push 在每次发送时使用。
//
// 与 open_id 版的差异：推送要按「推送应用」遍历，且要按收件人区分「可投递 / 缺 user_id /
// 工号碰撞 / 不在该企业」，因此这里原样返回 LarkUserIDIndex，由调用方按本人工号判定
// （语义见 LarkUserIDIndex）。
//
// 降级语义：员工服务未绑定返回「能力未就绪」错误（推送必须有身份数据 owner 才能寻址，
// 静默返回空索引会表现为「所有人都没身份」，掩盖装配缺陷）；批量查询失败向上传递。
func LarkUserIDResolverByApp(ctx context.Context, larkAppID string) (LarkUserIDIndex, error) {
	svc := BoundEmployeeService()
	if svc == nil {
		return LarkUserIDIndex{}, ErrServiceUnbound
	}
	return svc.MapOwnLarkUserIDsByEmployeeNo(ctx, larkAppID)
}

// BindTokenSecretProvider 装配拥有方插件的 JWT 密钥来源。
func BindTokenSecretProvider(fn SecretProvider) {
	bindMu.Lock()
	defer bindMu.Unlock()
	boundSecretFn = fn
}

// resolveSecret 通过绑定的提供方返回签名密钥，未绑定或提供方
// 返回空密钥时按失败关闭处理。
func resolveSecret(ctx context.Context) (string, error) {
	bindMu.RLock()
	fn := boundSecretFn
	bindMu.RUnlock()
	if fn == nil {
		return "", ErrTokenSecretUnavailable
	}
	secret, err := fn(ctx)
	if err != nil {
		return "", err
	}
	if secret == "" {
		return "", ErrTokenSecretUnavailable
	}
	return secret, nil
}
