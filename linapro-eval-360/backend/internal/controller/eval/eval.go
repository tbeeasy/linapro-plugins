// Package evalcontroller 实现 360 测评管理端控制器。
// 其路由运行在宿主 Auth()/Tenancy()/Permission() 链下。
package evalcontroller

import (
	"context"

	evalapi "lina-plugin-linapro-eval-360/backend/api/eval"
	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
	esvc "lina-plugin-linapro-eval-360/backend/internal/service/export"
	psvc "lina-plugin-linapro-eval-360/backend/internal/service/project"
	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
	rsvc "lina-plugin-linapro-eval-360/backend/internal/service/relation"
)

// pluginID 是本插件的唯一标识，用于探针回显。
const pluginID = "linapro-eval-360"

// ControllerV1 是管理端 v1 控制器，持有问卷、评价关系、项目与导出服务。
type ControllerV1 struct {
	questionnaireSvc qsvc.Service
	relationSvc      rsvc.Service
	projectSvc       psvc.Service
	exportSvc        esvc.Service
}

// NewV1 创建管理端控制器，注入问卷、评价关系、项目与导出服务。
func NewV1(questionnaireSvc qsvc.Service, relationSvc rsvc.Service, projectSvc psvc.Service, exportSvc esvc.Service) evalapi.IEvalV1 {
	return &ControllerV1{
		questionnaireSvc: questionnaireSvc,
		relationSvc:      relationSvc,
		projectSvc:       projectSvc,
		exportSvc:        exportSvc,
	}
}

// Ping 返回插件健康信息。能走到这里即证明管理端路由组、
// Auth/Tenancy/Permission 链均已正确挂载。
func (c *ControllerV1) Ping(ctx context.Context, _ *v1.PingReq) (res *v1.PingRes, err error) {
	return &v1.PingRes{Plugin: pluginID, Ready: true}, nil
}
