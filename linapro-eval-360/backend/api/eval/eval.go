// Package eval 定义 360 测评「管理端」控制器接口，经 group.Bind() 绑定。
// 管理端路由挂宿主 Auth()/Tenancy()/Permission() 链，复用宿主 sys_user 身份。
package eval

import (
	"context"

	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
)

// IEvalV1 是管理端 v1 控制器接口。骨架阶段仅含健康探针 Ping，
// Section 3 起扩展问卷/维度/题目/发布等能力。
type IEvalV1 interface {
	// Ping 返回插件健康信息，用于验证管理端路由与 Auth/Permission 链已就绪。
	Ping(ctx context.Context, req *v1.PingReq) (res *v1.PingRes, err error)

	// 问卷 CRUD 与结构树
	ListQuestionnaire(ctx context.Context, req *v1.ListQuestionnaireReq) (res *v1.ListQuestionnaireRes, err error)
	PageQuestionnaire(ctx context.Context, req *v1.PageQuestionnaireReq) (res *v1.PageQuestionnaireRes, err error)
	GetQuestionnaire(ctx context.Context, req *v1.GetQuestionnaireReq) (res *v1.GetQuestionnaireRes, err error)
	CreateQuestionnaire(ctx context.Context, req *v1.CreateQuestionnaireReq) (res *v1.CreateQuestionnaireRes, err error)
	UpdateQuestionnaire(ctx context.Context, req *v1.UpdateQuestionnaireReq) (res *v1.UpdateQuestionnaireRes, err error)
	DeleteQuestionnaire(ctx context.Context, req *v1.DeleteQuestionnaireReq) (res *v1.DeleteQuestionnaireRes, err error)

	// 维度 CRUD
	CreateDimension(ctx context.Context, req *v1.CreateDimensionReq) (res *v1.CreateDimensionRes, err error)
	UpdateDimension(ctx context.Context, req *v1.UpdateDimensionReq) (res *v1.UpdateDimensionRes, err error)
	DeleteDimension(ctx context.Context, req *v1.DeleteDimensionReq) (res *v1.DeleteDimensionRes, err error)

	// 题目 CRUD（连同选项/矩阵/可见性）
	CreateQuestion(ctx context.Context, req *v1.CreateQuestionReq) (res *v1.CreateQuestionRes, err error)
	UpdateQuestion(ctx context.Context, req *v1.UpdateQuestionReq) (res *v1.UpdateQuestionRes, err error)
	DeleteQuestion(ctx context.Context, req *v1.DeleteQuestionReq) (res *v1.DeleteQuestionRes, err error)
	// ReorderQuestions 按给定顺序重排维度下题目（插入新题后整体顺延）。
	ReorderQuestions(ctx context.Context, req *v1.ReorderQuestionsReq) (res *v1.ReorderQuestionsRes, err error)

	// 发布状态与编辑策略
	PublishQuestionnaire(ctx context.Context, req *v1.PublishQuestionnaireReq) (res *v1.PublishQuestionnaireRes, err error)
	UnpublishQuestionnaire(ctx context.Context, req *v1.UnpublishQuestionnaireReq) (res *v1.UnpublishQuestionnaireRes, err error)
	PublishProject(ctx context.Context, req *v1.PublishProjectReq) (res *v1.PublishProjectRes, err error)
	CopyQuestionnaire(ctx context.Context, req *v1.CopyQuestionnaireReq) (res *v1.CopyQuestionnaireRes, err error)
	ClearAnswers(ctx context.Context, req *v1.ClearAnswersReq) (res *v1.ClearAnswersRes, err error)

	// 项目与发送编排（Section 5）
	ListProject(ctx context.Context, req *v1.ListProjectReq) (res *v1.ListProjectRes, err error)
	GetProject(ctx context.Context, req *v1.GetProjectReq) (res *v1.GetProjectRes, err error)
	CreateProject(ctx context.Context, req *v1.CreateProjectReq) (res *v1.CreateProjectRes, err error)
	UpdateProject(ctx context.Context, req *v1.UpdateProjectReq) (res *v1.UpdateProjectRes, err error)
	DeleteProject(ctx context.Context, req *v1.DeleteProjectReq) (res *v1.DeleteProjectRes, err error)
	CloseProject(ctx context.Context, req *v1.CloseProjectReq) (res *v1.CloseProjectRes, err error)
	ReopenProject(ctx context.Context, req *v1.ReopenProjectReq) (res *v1.ReopenProjectRes, err error)
	SendProject(ctx context.Context, req *v1.SendProjectReq) (res *v1.SendProjectRes, err error)

	// 评价关系与角色（Section 4）
	ListRoles(ctx context.Context, req *v1.ListRolesReq) (res *v1.ListRolesRes, err error)
	UpdateRoleWeight(ctx context.Context, req *v1.UpdateRoleWeightReq) (res *v1.UpdateRoleWeightRes, err error)
	RenameRole(ctx context.Context, req *v1.RenameRoleReq) (res *v1.RenameRoleRes, err error)
	MergeRole(ctx context.Context, req *v1.MergeRoleReq) (res *v1.MergeRoleRes, err error)
	ImportTemplate(ctx context.Context, req *v1.ImportTemplateReq) (res *v1.ImportTemplateRes, err error)
	ImportRelations(ctx context.Context, req *v1.ImportRelationsReq) (res *v1.ImportRelationsRes, err error)
	ExportRelations(ctx context.Context, req *v1.ExportRelationsReq) (res *v1.ExportRelationsRes, err error)
	ListRelations(ctx context.Context, req *v1.ListRelationsReq) (res *v1.ListRelationsRes, err error)
	SearchEmployees(ctx context.Context, req *v1.SearchEmployeesReq) (res *v1.SearchEmployeesRes, err error)
	AddRelation(ctx context.Context, req *v1.AddRelationReq) (res *v1.AddRelationRes, err error)
	UpdateRelationRole(ctx context.Context, req *v1.UpdateRelationRoleReq) (res *v1.UpdateRelationRoleRes, err error)
	DeleteRelation(ctx context.Context, req *v1.DeleteRelationReq) (res *v1.DeleteRelationRes, err error)
	DeleteEvaluatee(ctx context.Context, req *v1.DeleteEvaluateeReq) (res *v1.DeleteEvaluateeRes, err error)

	// 明细导出（Section 7，仅管理端）
	ExportByAnswer(ctx context.Context, req *v1.ExportByAnswerReq) (res *v1.ExportByAnswerRes, err error)
	ExportByScore(ctx context.Context, req *v1.ExportByScoreReq) (res *v1.ExportByScoreRes, err error)
}
