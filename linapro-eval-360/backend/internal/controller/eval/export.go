// export.go 实现 360 测评管理端「明细导出」控制器方法。
// 两个独立按钮、各自直出单个 Excel，不打包 ZIP；走 WriteOver + ExitAll。
package evalcontroller

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"

	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
)

// ExportByAnswer 按答案导出 Excel。
func (c *ControllerV1) ExportByAnswer(ctx context.Context, req *v1.ExportByAnswerReq) (res *v1.ExportByAnswerRes, err error) {
	data, err := c.exportSvc.ExportByAnswer(ctx, req.QuestionnaireId)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	// 按答案导出：eval360-export-by-answer.xlsx（RFC5987 中文名）。
	r.Response.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''%E6%8C%89%E7%AD%94%E6%A1%88%E5%AF%BC%E5%87%BA.xlsx")
	r.Response.WriteOver(data)
	r.ExitAll()
	return nil, nil
}

// ExportByScore 按得分导出 Excel。
func (c *ControllerV1) ExportByScore(ctx context.Context, req *v1.ExportByScoreReq) (res *v1.ExportByScoreRes, err error) {
	data, err := c.exportSvc.ExportByScore(ctx, req.QuestionnaireId)
	if err != nil {
		return nil, err
	}
	r := g.RequestFromCtx(ctx)
	r.Response.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	// 按得分导出：eval360-export-by-score.xlsx（RFC5987 中文名）。
	r.Response.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''%E6%8C%89%E5%BE%97%E5%88%86%E5%AF%BC%E5%87%BA.xlsx")
	r.Response.WriteOver(data)
	r.ExitAll()
	return nil, nil
}
