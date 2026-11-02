// emp.go 实现 360 测评员工端控制器：我的待办（按问卷聚合）、按问卷取批量题集、批量提交作答。
package empcontroller

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"

	empapi "lina-plugin-linapro-eval-360/backend/api/emp"
	v1 "lina-plugin-linapro-eval-360/backend/api/emp/v1"
	esvc "lina-plugin-linapro-eval-360/backend/internal/service/employee"
	qsvc "lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
)

// timePtrToMillis 把 *time.Time 投影为 Unix 毫秒时间戳（nil 透传 nil）。
func timePtrToMillis(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

// ControllerV1 是员工端 v1 控制器，持有员工端作答服务。
type ControllerV1 struct {
	employeeSvc esvc.Service
}

// NewV1 创建员工端控制器，注入员工端作答服务。
func NewV1(employeeSvc esvc.Service) empapi.IEmpV1 {
	return &ControllerV1{employeeSvc: employeeSvc}
}

// Whoami 读取 EmpAuth 注入的 EmpContext 并回显。
func (c *ControllerV1) Whoami(ctx context.Context, _ *v1.WhoamiReq) (res *v1.WhoamiRes, err error) {
	empCtx, ok := empcap.EmpFromContext(ctx)
	if !ok {
		return nil, gerror.New("缺少员工上下文：EmpAuth 未注入身份")
	}
	return &v1.WhoamiRes{TenantID: empCtx.TenantID, EmpID: empCtx.EmpID}, nil
}

// Todo 我的待办列表（按问卷聚合）。
func (c *ControllerV1) Todo(ctx context.Context, _ *v1.TodoReq) (res *v1.TodoRes, err error) {
	list, err := c.employeeSvc.ListTodo(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*v1.TodoItem, 0, len(list))
	for _, t := range list {
		ees := make([]*v1.TodoEvaluatee, 0, len(t.Evaluatees))
		for _, e := range t.Evaluatees {
			ees = append(ees, &v1.TodoEvaluatee{
				RelationID: e.RelationID, EvaluateeName: e.EvaluateeName,
				JobTitle: e.JobTitle, Department: e.Department,
				RoleName: e.RoleName, Submitted: e.Submitted,
			})
		}
		out = append(out, &v1.TodoItem{
			QuestionnaireID: t.QuestionnaireID,
			ProjectID:       t.ProjectID, ProjectName: t.ProjectName,
			QuestionnaireTitle: t.QuestionnaireTitle,
			StartAt:            timePtrToMillis(t.StartAt), EndAt: timePtrToMillis(t.EndAt),
			Total: t.Total, CompletedCount: t.CompletedCount, DerivedStatus: string(t.DerivedStatus),
			Evaluatees: ees,
		})
	}
	return &v1.TodoRes{List: out}, nil
}

// Questions 按问卷取批量题集。
func (c *ControllerV1) Questions(ctx context.Context, req *v1.QuestionsReq) (res *v1.QuestionsRes, err error) {
	set, err := c.employeeSvc.GetQuestionnaireSet(ctx, req.QuestionnaireId)
	if err != nil {
		return nil, err
	}
	return &v1.QuestionsRes{Set: toQuestionnaireSetDTO(set)}, nil
}

// Submit 批量提交作答。
func (c *ControllerV1) Submit(ctx context.Context, req *v1.SubmitReq) (res *v1.SubmitRes, err error) {
	items := make([]esvc.SubmitItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		answers := make([]esvc.AnswerInput, 0, len(it.Answers))
		for _, a := range it.Answers {
			answers = append(answers, esvc.AnswerInput{
				QuestionID: a.QuestionId, OptionID: a.OptionId, TextValue: a.TextValue,
			})
		}
		items = append(items, esvc.SubmitItemInput{RelationID: it.RelationId, Answers: answers})
	}
	result, err := c.employeeSvc.SubmitBatch(ctx, esvc.SubmitBatchInput{
		QuestionnaireID: req.QuestionnaireId, DurationSeconds: req.DurationSeconds, Items: items,
	})
	if err != nil {
		return nil, err
	}
	return &v1.SubmitRes{
		SubmittedCount: result.SubmittedCount, SubmittedAt: timePtrToMillis(result.SubmittedAt), Idempotent: result.Idempotent,
	}, nil
}

// toQuestionnaireSetDTO 把 service 层 QuestionnaireSet 转为 API 层 DTO。
func toQuestionnaireSetDTO(set *esvc.QuestionnaireSet) *v1.QuestionnaireSet {
	if set == nil {
		return nil
	}
	out := &v1.QuestionnaireSet{
		QuestionnaireID: set.QuestionnaireID, QuestionnaireTitle: set.QuestionnaireTitle,
		Evaluatees:       make([]v1.EvaluateeRow, 0, len(set.Evaluatees)),
		Dimensions:       make([]v1.DimensionNode, 0, len(set.Dimensions)),
		SubmittedAnswers: make([]v1.SubmittedAnswer, 0, len(set.SubmittedAnswers)),
		Closed:           set.Closed,
	}
	for _, e := range set.Evaluatees {
		out.Evaluatees = append(out.Evaluatees, v1.EvaluateeRow{
			RelationID: e.RelationID, EvaluateeName: e.EvaluateeName, RoleName: e.RoleName, Submitted: e.Submitted,
		})
	}
	for _, d := range set.Dimensions {
		dn := v1.DimensionNode{Id: d.Id, Name: d.Name, Sort: d.Sort, Questions: make([]v1.QuestionNode, 0, len(d.Questions))}
		for _, sq := range d.Questions {
			qq := sq.Node
			dn.Questions = append(dn.Questions, v1.QuestionNode{
				Id: qq.Id, DimensionId: qq.DimensionId, Type: qq.Type, Title: qq.Title,
				Description: qq.Description,
				Required:    qq.Required, Sort: qq.Sort,
				Options:        toOptionNodes(qq.Options),
				MatrixColumns:  toMatrixColumnNodes(qq.MatrixColumns),
				RowRelationIds: sq.RowRelationIDs,
			})
		}
		out.Dimensions = append(out.Dimensions, dn)
	}
	for _, a := range set.SubmittedAnswers {
		out.SubmittedAnswers = append(out.SubmittedAnswers, v1.SubmittedAnswer{
			RelationID: a.RelationID, QuestionID: a.QuestionID, OptionID: a.OptionID, TextValue: a.TextValue,
		})
	}
	return out
}

// toOptionNodes 把 service 层 OptionNode 切片转为 API 层 DTO。
func toOptionNodes(in []*qsvc.OptionNode) []v1.OptionNode {
	out := make([]v1.OptionNode, 0, len(in))
	for _, o := range in {
		out = append(out, v1.OptionNode{
			Id: o.Id, Label: o.Label, Score: o.Score, AllowText: o.AllowText, Sort: o.Sort,
		})
	}
	return out
}

// toMatrixColumnNodes 把 service 层 MatrixColumnNode 切片转为 API 层 DTO。
// 评语配置（开关/必填/提示语/预设/自定义）随节点原样透传给员工端作答页。
func toMatrixColumnNodes(in []*qsvc.MatrixColumnNode) []v1.MatrixColumnNode {
	out := make([]v1.MatrixColumnNode, 0, len(in))
	for _, c := range in {
		out = append(out, v1.MatrixColumnNode{
			Id: c.Id, Label: c.Label, Score: c.Score, Sort: c.Sort,
			AllowComment:       c.AllowComment,
			CommentRequired:    c.CommentRequired,
			CommentPlaceholder: c.CommentPlaceholder,
			CommentPresets:     c.CommentPresets,
			CommentAllowCustom: c.CommentAllowCustom,
		})
	}
	return out
}
