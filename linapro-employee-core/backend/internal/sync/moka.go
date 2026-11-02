// moka.go 说明 MokaFetcher 的生产装配。
// MokaFetcher、MokaEmployee、LarkAppFetcher 和 LarkUser 声明在
// syncer.go 中。plugin.go 中的生产适配器包装 *hcm.Client 并把
// hcm.HCMEmployee 转换为 sync.MokaEmployee，使本包不依赖
// linapro-moka-hcm。
package sync
