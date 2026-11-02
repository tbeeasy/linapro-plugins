// Package export 实现 360 测评「明细导出」管理端服务（eval-export）。
// 仅挂管理端 Auth()/Permission()，无员工端入口。两张明细各直出单个 Excel，不打包 ZIP。
//
// 设计要点（见 openspec/changes/linapro-eval-360/design.md D7）：
//   - 固定前导列：序号 | 提交时间 | 所用时间 | 关系角色 | 被评价对象（无「来源」「来源详情」「评价者」）。
//   - 按答案表：全部题原文；矩阵一题一列；选项级评语并入该题单元格、以【】包裹；开放题独立列；未作答留空；只导已提交。
//   - 按得分表：仅含计分题（单选、矩阵）；分值写数字单元格；0 分即「无法评价」与未作答均留空；不含文本题与评语；只导已提交。
//   - 所用时间：前端计时上传 duration_seconds，后端原样存、导出原样显示。
package export

import (
	"context"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// ExportKind 标识两种导出。
type ExportKind string

const (
	KindByAnswer ExportKind = "byAnswer" // 按答案导出
	KindByScore  ExportKind = "byScore"  // 按得分导出
)

// Service 定义明细导出的管理端能力。
type Service interface {
	// ExportByAnswer 按答案导出（全部题原文，评语并入单元格【】包裹）。
	ExportByAnswer(ctx context.Context, questionnaireID int64) ([]byte, error)
	// ExportByScore 按得分导出（仅计分题，分值为数字单元格，0 分即无法评价与未作答均留空）。
	ExportByScore(ctx context.Context, questionnaireID int64) ([]byte, error)
}

// 编译期断言 serviceImpl 实现 Service。
var _ Service = (*serviceImpl)(nil)

// serviceImpl 是 Service 的默认实现，依赖 empcap（取员工姓名/工号）。
type serviceImpl struct{}

// New 构造导出服务。empcap 经 BoundEmployeeService() 懒解析。
func New() Service {
	return &serviceImpl{}
}

// empSvc 返回 empcap 绑定的员工服务，未绑定时返回 nil。
func (s *serviceImpl) empSvc() empcap.Service {
	return empcap.BoundEmployeeService()
}
