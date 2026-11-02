// questionnaire_impl.go 实现问卷 CRUD 与完整结构树读取。
package questionnaire

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/shopspring/decimal"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
	"lina-plugin-linapro-eval-360/backend/internal/service/textutil"
)

// mustGetQuestionnaire 读取问卷，不存在返回 CodeQuestionnaireNotFound。
func (s *serviceImpl) mustGetQuestionnaire(ctx context.Context, id int64) (*entitymodel.Questionnaire, error) {
	var q *entitymodel.Questionnaire
	if err := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().Id, id).
		Scan(&q); err != nil {
		return nil, err
	}
	if q == nil {
		return nil, bizerr.NewCode(CodeQuestionnaireNotFound)
	}
	return q, nil
}

// hasSubmissions 判断问卷是否已有任何作答（决定编辑三策略是否触发白名单强校验）。
func (s *serviceImpl) hasSubmissions(ctx context.Context, questionnaireID int64) (bool, error) {
	cnt, err := dao.Submission.Ctx(ctx).
		Where(dao.Submission.Columns().QuestionnaireId, questionnaireID).
		Count()
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// editRestricted 判断问卷编辑是否受限：**已发布 OR 已有作答**。
// 已发布保护在途填写者——系统无快照、取题与计分直读 live 结构，问卷一旦发布即可能有评价者
// 正在填写（尚未提交、hasSubmissions 数不到），此时破坏性改结构会与在途作答错位；
// 有作答则保护已提交答卷的引用完整性与历史得分。任一成立即触发编辑三策略白名单强校验。
func (s *serviceImpl) editRestricted(ctx context.Context, questionnaireID int64) (bool, error) {
	published, err := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().Id, questionnaireID).
		Fields(dao.Questionnaire.Columns().Published).Value()
	if err != nil {
		return false, err
	}
	if published.Int() == publishedYes {
		return true, nil
	}
	return s.hasSubmissions(ctx, questionnaireID)
}

// ListByProject 返回某项目下的问卷列表（按 sort、id 升序）。
func (s *serviceImpl) ListByProject(ctx context.Context, projectID int64) ([]*QuestionnaireListItem, error) {
	cols := dao.Questionnaire.Columns()
	var rows []*entitymodel.Questionnaire
	if err := dao.Questionnaire.Ctx(ctx).
		Where(cols.ProjectId, projectID).
		OrderAsc(cols.Sort).OrderAsc(cols.Id).
		Scan(&rows); err != nil {
		return nil, err
	}
	items := make([]*QuestionnaireListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, toListItem(r))
	}
	return items, nil
}

// PageByProject 分页返回某项目下的问卷（可按标题模糊搜索），返回当页数据与总数。
func (s *serviceImpl) PageByProject(ctx context.Context, projectID int64, keyword string, pageNum, pageSize int) ([]*QuestionnaireListItem, int, error) {
	cols := dao.Questionnaire.Columns()
	m := dao.Questionnaire.Ctx(ctx).Where(cols.ProjectId, projectID)
	if kw := strings.TrimSpace(keyword); kw != "" {
		m = m.WhereLike(cols.Title, "%"+kw+"%")
	}
	total, err := m.Count()
	if err != nil {
		return nil, 0, err
	}
	if pageNum <= 0 {
		pageNum = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	var rows []*entitymodel.Questionnaire
	if err := m.OrderAsc(cols.Sort).OrderAsc(cols.Id).
		Page(pageNum, pageSize).Scan(&rows); err != nil {
		return nil, 0, err
	}
	items := make([]*QuestionnaireListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, toListItem(r))
	}
	return items, total, nil
}

// toListItem 把问卷实体映射为列表项 DTO。
func toListItem(r *entitymodel.Questionnaire) *QuestionnaireListItem {
	return &QuestionnaireListItem{
		Id:          r.Id,
		ProjectId:   r.ProjectId,
		Title:       r.Title,
		Description: r.Description,
		Published:   r.Published,
		Sort:        r.Sort,
		CreatedAt:   r.CreatedAt,
	}
}

// GetDetail 组装并返回问卷完整 live 结构树。
func (s *serviceImpl) GetDetail(ctx context.Context, id int64) (*QuestionnaireDetail, error) {
	q, err := s.mustGetQuestionnaire(ctx, id)
	if err != nil {
		return nil, err
	}

	// 维度
	var dims []*entitymodel.Dimension
	if err = dao.Dimension.Ctx(ctx).
		Where(dao.Dimension.Columns().QuestionnaireId, id).
		OrderAsc(dao.Dimension.Columns().Sort).OrderAsc(dao.Dimension.Columns().Id).
		Scan(&dims); err != nil {
		return nil, err
	}

	// 题目
	var questions []*entitymodel.Question
	if err = dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().QuestionnaireId, id).
		OrderAsc(dao.Question.Columns().Sort).OrderAsc(dao.Question.Columns().Id).
		Scan(&questions); err != nil {
		return nil, err
	}
	questionIDs := make([]int64, 0, len(questions))
	for _, qq := range questions {
		questionIDs = append(questionIDs, qq.Id)
	}

	// 从属结构（选项/矩阵列/可见性/排除名单），一次性按 question_id 批量取回。
	// 矩阵已无手填行：矩阵行由被评估人清单动态渲染，此处不再加载 matrix_row。
	var (
		options  []*entitymodel.QuestionOption
		columns  []*entitymodel.MatrixColumn
		vis      []*entitymodel.QuestionVisibility
		excluded []*entitymodel.QuestionExcludedEvaluator
	)
	if len(questionIDs) > 0 {
		if err = dao.QuestionOption.Ctx(ctx).
			WhereIn(dao.QuestionOption.Columns().QuestionId, questionIDs).
			OrderAsc(dao.QuestionOption.Columns().Sort).OrderAsc(dao.QuestionOption.Columns().Id).
			Scan(&options); err != nil {
			return nil, err
		}
		if err = dao.MatrixColumn.Ctx(ctx).
			WhereIn(dao.MatrixColumn.Columns().QuestionId, questionIDs).
			OrderAsc(dao.MatrixColumn.Columns().Sort).OrderAsc(dao.MatrixColumn.Columns().Id).
			Scan(&columns); err != nil {
			return nil, err
		}
		if err = dao.QuestionVisibility.Ctx(ctx).
			WhereIn(dao.QuestionVisibility.Columns().QuestionId, questionIDs).
			Scan(&vis); err != nil {
			return nil, err
		}
		if err = dao.QuestionExcludedEvaluator.Ctx(ctx).
			WhereIn(dao.QuestionExcludedEvaluator.Columns().QuestionId, questionIDs).
			OrderAsc(dao.QuestionExcludedEvaluator.Columns().Id).
			Scan(&excluded); err != nil {
			return nil, err
		}
	}

	// 角色
	var roles []*entitymodel.Role
	if err = dao.Role.Ctx(ctx).
		Where(dao.Role.Columns().QuestionnaireId, id).
		OrderAsc(dao.Role.Columns().Id).
		Scan(&roles); err != nil {
		return nil, err
	}

	// 建立 question_id → 各从属结构 的索引。
	optionsByQ := make(map[int64][]*OptionNode)
	for _, o := range options {
		optionsByQ[o.QuestionId] = append(optionsByQ[o.QuestionId], &OptionNode{
			Id: o.Id, Label: o.Label, Score: decimalString(o.Score),
			AllowText: o.AllowText, Sort: o.Sort,
		})
	}
	colsByQ := make(map[int64][]*MatrixColumnNode)
	for _, c := range columns {
		colsByQ[c.QuestionId] = append(colsByQ[c.QuestionId], &MatrixColumnNode{
			Id: c.Id, Label: c.Label, Score: decimalString(c.Score), Sort: c.Sort,
			AllowComment:       c.AllowComment,
			CommentRequired:    c.CommentRequired,
			CommentPlaceholder: c.CommentPlaceholder,
			CommentPresets:     decodeCommentPresets(c.CommentPresets),
			CommentAllowCustom: c.CommentAllowCustom,
		})
	}
	visByQ := make(map[int64][]int64)
	for _, v := range vis {
		visByQ[v.QuestionId] = parseCSVInt64(v.VisibleRoleIds)
	}
	excludedByQ := make(map[int64][]int64)
	for _, e := range excluded {
		excludedByQ[e.QuestionId] = append(excludedByQ[e.QuestionId], e.EvaluatorEmpId)
	}

	// 建立 dimension_id → 题目节点 的索引。
	questionsByDim := make(map[int64][]*QuestionNode)
	for _, qq := range questions {
		node := &QuestionNode{
			Id:                      qq.Id,
			DimensionId:             qq.DimensionId,
			Type:                    qq.Type,
			Title:                   qq.Title,
			Description:             qq.Description,
			Required:                qq.Required,
			Sort:                    qq.Sort,
			Options:                 optionsByQ[qq.Id],
			MatrixColumns:           colsByQ[qq.Id],
			VisibleRoleIds:          visByQ[qq.Id],
			ExcludedEvaluatorEmpIds: excludedByQ[qq.Id],
		}
		ensureNodeSlices(node)
		questionsByDim[qq.DimensionId] = append(questionsByDim[qq.DimensionId], node)
	}

	detail := &QuestionnaireDetail{
		Id: q.Id, ProjectId: q.ProjectId, Title: q.Title,
		Description: q.Description, Published: q.Published, Sort: q.Sort,
		Dimensions: make([]*DimensionNode, 0, len(dims)),
		Roles:      make([]*RoleNode, 0, len(roles)),
	}
	for _, d := range dims {
		qs := questionsByDim[d.Id]
		if qs == nil {
			qs = []*QuestionNode{}
		}
		detail.Dimensions = append(detail.Dimensions, &DimensionNode{
			Id: d.Id, Name: d.Name, Sort: d.Sort, Questions: qs,
		})
	}
	for _, r := range roles {
		detail.Roles = append(detail.Roles, &RoleNode{
			Id: r.Id, Name: r.Name, Weight: decimalString(r.Weight),
		})
	}
	// 是否已有作答：供前端按 published||hasAnswers 判断编辑是否受限。
	hasAns, err := s.hasSubmissions(ctx, id)
	if err != nil {
		return nil, err
	}
	detail.HasAnswers = hasAns
	return detail, nil
}

// CreateQuestionnaire 新建问卷（默认未发布）。
// 标题先归一化，在同项目内查重；撞名或纯空白标题直接拒绝。
func (s *serviceImpl) CreateQuestionnaire(ctx context.Context, in CreateQuestionnaireInput) (int64, error) {
	title := textutil.NormalizeName(in.Title)
	if title == "" {
		return 0, bizerr.NewCode(CodeQuestionnaireTitleEmpty)
	}
	dup, err := s.titleExists(ctx, in.ProjectID, title, 0)
	if err != nil {
		return 0, err
	}
	if dup {
		return 0, bizerr.NewCode(CodeQuestionnaireTitleExists)
	}
	id, err := dao.Questionnaire.Ctx(ctx).Data(do.Questionnaire{
		ProjectId:   in.ProjectID,
		Title:       title,
		Description: in.Description,
		Published:   publishedNo,
		Sort:        in.Sort,
	}).InsertAndGetId()
	if err != nil {
		// 并发穿透：预检与写入之间的竞态由唯一索引兜底。
		if isUniqueViolation(err) {
			return 0, bizerr.NewCode(CodeQuestionnaireTitleExists)
		}
		return 0, err
	}
	return id, nil
}

// UpdateQuestionnaire 就地修改问卷元信息（草稿/收集期间均可）。
// 改名时在同项目内查重（排除自身）。
func (s *serviceImpl) UpdateQuestionnaire(ctx context.Context, in UpdateQuestionnaireInput) error {
	q, err := s.mustGetQuestionnaire(ctx, in.Id)
	if err != nil {
		return err
	}
	data := g.Map{}
	if in.Title != nil {
		title := textutil.NormalizeName(*in.Title)
		if title == "" {
			return bizerr.NewCode(CodeQuestionnaireTitleEmpty)
		}
		// 与当前标题一致 → 幂等放行，不做查重（否则会与自身撞名）。
		if title != q.Title {
			dup, dErr := s.titleExists(ctx, q.ProjectId, title, q.Id)
			if dErr != nil {
				return dErr
			}
			if dup {
				return bizerr.NewCode(CodeQuestionnaireTitleExists)
			}
		}
		data[dao.Questionnaire.Columns().Title] = title
	}
	if in.Description != nil {
		data[dao.Questionnaire.Columns().Description] = *in.Description
	}
	if in.Sort != nil {
		data[dao.Questionnaire.Columns().Sort] = *in.Sort
	}
	if len(data) == 0 {
		return nil
	}
	_, err = dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().Id, in.Id).
		Data(data).Update()
	if err != nil && isUniqueViolation(err) {
		return bizerr.NewCode(CodeQuestionnaireTitleExists)
	}
	return err
}

// DeleteQuestionnaire 删除问卷及其结构。已发布或存在作答时拒绝（先取消发布 / 清空作答后再删）。
func (s *serviceImpl) DeleteQuestionnaire(ctx context.Context, id int64) error {
	if _, err := s.mustGetQuestionnaire(ctx, id); err != nil {
		return err
	}
	restricted, err := s.editRestricted(ctx, id)
	if err != nil {
		return err
	}
	if restricted {
		return bizerr.NewCode(CodeEditRequiresStrategy)
	}
	return dao.Questionnaire.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 先删题目从属结构（按题目集合），再删题目、维度、角色，最后删问卷本体。
		idVals, err := dao.Question.Ctx(ctx).
			Where(dao.Question.Columns().QuestionnaireId, id).
			Fields(dao.Question.Columns().Id).Array()
		if err != nil {
			return err
		}
		questionIDs := make([]int64, 0, len(idVals))
		for _, v := range idVals {
			questionIDs = append(questionIDs, v.Int64())
		}
		if len(questionIDs) > 0 {
			if err := s.deleteQuestionChildren(ctx, questionIDs); err != nil {
				return err
			}
			if _, err := dao.Question.Ctx(ctx).
				WhereIn(dao.Question.Columns().Id, questionIDs).Delete(); err != nil {
				return err
			}
		}
		for _, del := range []struct {
			model *gdb.Model
			col   string
		}{
			{dao.Dimension.Ctx(ctx), dao.Dimension.Columns().QuestionnaireId},
			{dao.Role.Ctx(ctx), dao.Role.Columns().QuestionnaireId},
		} {
			if _, err := del.model.
				Where(del.col, id).Delete(); err != nil {
				return err
			}
		}
		_, err = dao.Questionnaire.Ctx(ctx).
			Where(dao.Questionnaire.Columns().Id, id).Delete()
		return err
	})
}

// deleteQuestionChildren 删除一批题目的全部从属结构（选项/矩阵行列/可见性/排除名单）。
func (s *serviceImpl) deleteQuestionChildren(ctx context.Context, questionIDs []int64) error {
	children := []struct {
		model *gdb.Model
		col   string
	}{
		{dao.QuestionOption.Ctx(ctx), dao.QuestionOption.Columns().QuestionId},
		{dao.MatrixRow.Ctx(ctx), dao.MatrixRow.Columns().QuestionId},
		{dao.MatrixColumn.Ctx(ctx), dao.MatrixColumn.Columns().QuestionId},
		{dao.QuestionVisibility.Ctx(ctx), dao.QuestionVisibility.Columns().QuestionId},
		{dao.QuestionExcludedEvaluator.Ctx(ctx), dao.QuestionExcludedEvaluator.Columns().QuestionId},
	}
	for _, c := range children {
		if _, err := c.model.
			WhereIn(c.col, questionIDs).Delete(); err != nil {
			return err
		}
	}
	return nil
}

// decimalString 把 NUMERIC 指针转为字符串，nil 记 "0"。
func decimalString(d *decimal.Decimal) string {
	if d == nil {
		return "0"
	}
	return d.String()
}

// ensureNodeSlices 把题目节点的空切片补齐为非 nil，便于前端与 JSON 稳定。
func ensureNodeSlices(node *QuestionNode) {
	if node.Options == nil {
		node.Options = []*OptionNode{}
	}
	if node.MatrixColumns == nil {
		node.MatrixColumns = []*MatrixColumnNode{}
	}
	if node.VisibleRoleIds == nil {
		node.VisibleRoleIds = []int64{}
	}
	if node.ExcludedEvaluatorEmpIds == nil {
		node.ExcludedEvaluatorEmpIds = []int64{}
	}
}
