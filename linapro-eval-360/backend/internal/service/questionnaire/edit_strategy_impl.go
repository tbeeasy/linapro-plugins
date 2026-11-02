// edit_strategy_impl.go 实现编辑三策略中的：②删除答案（清空作答并重置关系完成态）、
// ③复制问卷（深拷贝结构 + 被评估人→评价关系树为未发布新问卷，不复制答案）。
// 策略①保留答案的白名单校验见 question_impl.go 的 guardWhitelistEdit。
package questionnaire

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
	"lina-plugin-linapro-eval-360/backend/internal/service/textutil"
)

// ClearAnswers 策略②：清空该问卷全部作答（答案 + 作答）、重置相关评价关系完成态，
// 并将问卷**转为未发布**。下线是必须的：受限判据为「已发布 OR 有作答」，若仅清作答而仍
// 处于发布态则依旧受限，无法兑现策略②「删除答案后不受限编辑」；下线后也不再有新的在途填写者。
func (s *serviceImpl) ClearAnswers(ctx context.Context, id int64) error {
	if _, err := s.mustGetQuestionnaire(ctx, id); err != nil {
		return err
	}
	return dao.Questionnaire.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		if _, err := dao.Answer.Ctx(ctx).
			Where(dao.Answer.Columns().QuestionnaireId, id).Delete(); err != nil {
			return err
		}
		if _, err := dao.Submission.Ctx(ctx).
			Where(dao.Submission.Columns().QuestionnaireId, id).Delete(); err != nil {
			return err
		}
		if _, err := dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().QuestionnaireId, id).
			Data(map[string]any{
				dao.Relation.Columns().Completed:   0,
				dao.Relation.Columns().CompletedAt: nil,
			}).Update(); err != nil {
			return err
		}
		_, err := dao.Questionnaire.Ctx(ctx).
			Where(dao.Questionnaire.Columns().Id, id).
			Data(map[string]any{dao.Questionnaire.Columns().Published: publishedNo}).Update()
		return err
	})
}

// CopyQuestionnaire 策略③：单事务深拷贝【问卷结构 + 被评估人→评价关系树】为新问卷。
// 新问卷默认未发布、不自动推送；不复制答案（新关系一律未填）；原问卷不受影响。
//
// title 由调用方（前端弹窗）提供：同名问卷在同项目内唯一，故复制件不能沿用源标题。
// 标题先归一化再在同项目内查重（不排除任何问卷——源标题本身即占用者）。
func (s *serviceImpl) CopyQuestionnaire(ctx context.Context, id int64, title string) (int64, error) {
	src, err := s.mustGetQuestionnaire(ctx, id)
	if err != nil {
		return 0, err
	}
	projectID := src.ProjectId

	normTitle := textutil.NormalizeName(title)
	if normTitle == "" {
		return 0, bizerr.NewCode(CodeQuestionnaireTitleEmpty)
	}
	dup, err := s.titleExists(ctx, projectID, normTitle, 0)
	if err != nil {
		return 0, err
	}
	if dup {
		return 0, bizerr.NewCode(CodeQuestionnaireTitleExists)
	}

	var newID int64
	err = dao.Questionnaire.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 1. 问卷本体（未发布）。
		newID, err = dao.Questionnaire.Ctx(ctx).Data(do.Questionnaire{
			ProjectId:   projectID,
			Title:       normTitle,
			Description: src.Description,
			Published:   publishedNo,
			Sort:        src.Sort,
		}).InsertAndGetId()
		if err != nil {
			// 并发穿透：预检与写入之间的竞态由唯一索引兜底。
			if isUniqueViolation(err) {
				return bizerr.NewCode(CodeQuestionnaireTitleExists)
			}
			return err
		}

		// 2. 角色 → roleMap（旧 id → 新 id），供可见性 CSV 与评价关系重映射。
		roleMap := make(map[int64]int64)
		var roles []*entitymodel.Role
		if err = dao.Role.Ctx(ctx).
			Where(dao.Role.Columns().QuestionnaireId, id).Scan(&roles); err != nil {
			return err
		}
		for _, r := range roles {
			nid, insErr := dao.Role.Ctx(ctx).Data(do.Role{
				QuestionnaireId: newID, Name: r.Name, Weight: r.Weight,
			}).InsertAndGetId()
			if insErr != nil {
				return insErr
			}
			roleMap[r.Id] = nid
		}

		// 3. 被评估人 → evalMap（职务/部门一并复制）。
		evalMap := make(map[int64]int64)
		var evaluatees []*entitymodel.Evaluatee
		if err = dao.Evaluatee.Ctx(ctx).
			Where(dao.Evaluatee.Columns().QuestionnaireId, id).Scan(&evaluatees); err != nil {
			return err
		}
		for _, e := range evaluatees {
			nid, insErr := dao.Evaluatee.Ctx(ctx).Data(do.Evaluatee{
				ProjectId: projectID, QuestionnaireId: newID,
				EvaluateeEmpId: e.EvaluateeEmpId,
				JobTitle:       e.JobTitle,
				Department:     e.Department,
			}).InsertAndGetId()
			if insErr != nil {
				return insErr
			}
			evalMap[e.Id] = nid
		}

		// 4. 评价关系（完成态清零、不复制答案）。
		var relations []*entitymodel.Relation
		if err = dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().QuestionnaireId, id).Scan(&relations); err != nil {
			return err
		}
		for _, rel := range relations {
			if _, insErr := dao.Relation.Ctx(ctx).Data(do.Relation{
				ProjectId: projectID, QuestionnaireId: newID,
				EvaluateeId: evalMap[rel.EvaluateeId], EvaluatorEmpId: rel.EvaluatorEmpId,
				RoleId: roleMap[rel.RoleId], Source: rel.Source, Completed: 0,
			}).Insert(); insErr != nil {
				return insErr
			}
		}

		// 5. 维度 → dimMap。
		dimMap := make(map[int64]int64)
		var dims []*entitymodel.Dimension
		if err = dao.Dimension.Ctx(ctx).
			Where(dao.Dimension.Columns().QuestionnaireId, id).Scan(&dims); err != nil {
			return err
		}
		for _, d := range dims {
			nid, insErr := dao.Dimension.Ctx(ctx).Data(do.Dimension{
				QuestionnaireId: newID, Name: d.Name, Sort: d.Sort,
			}).InsertAndGetId()
			if insErr != nil {
				return insErr
			}
			dimMap[d.Id] = nid
		}

		// 6. 题目及其从属结构（选项/矩阵行列/可见性/排除名单）。
		var questions []*entitymodel.Question
		if err = dao.Question.Ctx(ctx).
			Where(dao.Question.Columns().QuestionnaireId, id).Scan(&questions); err != nil {
			return err
		}
		for _, srcQ := range questions {
			newQID, insErr := dao.Question.Ctx(ctx).Data(do.Question{
				QuestionnaireId: newID, DimensionId: dimMap[srcQ.DimensionId],
				Type: srcQ.Type, Title: srcQ.Title, Description: srcQ.Description,
				Required: srcQ.Required, Sort: srcQ.Sort,
			}).InsertAndGetId()
			if insErr != nil {
				return insErr
			}
			if err = s.copyQuestionChildren(ctx, srcQ.Id, newQID, roleMap); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newID, nil
}

// copyQuestionChildren 深拷贝单题的选项、矩阵列、可见性（重映射角色 id）与受限评价者名单（原样拷贝 EmpID）。
func (s *serviceImpl) copyQuestionChildren(ctx context.Context, srcQID, newQID int64, roleMap map[int64]int64) error {
	// 选项
	var options []*entitymodel.QuestionOption
	if err := dao.QuestionOption.Ctx(ctx).
		Where(dao.QuestionOption.Columns().QuestionId, srcQID).Scan(&options); err != nil {
		return err
	}
	for _, o := range options {
		if _, err := dao.QuestionOption.Ctx(ctx).Data(do.QuestionOption{
			QuestionId: newQID, Label: o.Label, Score: o.Score,
			AllowText: o.AllowText, Sort: o.Sort,
		}).Insert(); err != nil {
			return err
		}
	}
	// 矩阵列（矩阵已无手填行，行由被评估人清单动态渲染，无需拷贝）
	var cols []*entitymodel.MatrixColumn
	if err := dao.MatrixColumn.Ctx(ctx).
		Where(dao.MatrixColumn.Columns().QuestionId, srcQID).Scan(&cols); err != nil {
		return err
	}
	for _, c := range cols {
		// 评语配置（允许评语/必填/提示语/预设填空/允许自定义）随列一并深拷贝；
		// comment_presets 为 JSON 文本，原样复制无需重新编解码。
		if _, err := dao.MatrixColumn.Ctx(ctx).Data(do.MatrixColumn{
			QuestionId: newQID, Label: c.Label, Score: c.Score,
			Sort:               c.Sort,
			AllowComment:       c.AllowComment,
			CommentRequired:    c.CommentRequired,
			CommentPlaceholder: c.CommentPlaceholder,
			CommentPresets:     c.CommentPresets,
			CommentAllowCustom: c.CommentAllowCustom,
		}).Insert(); err != nil {
			return err
		}
	}
	// 可见性（重映射角色 id）
	var vis []*entitymodel.QuestionVisibility
	if err := dao.QuestionVisibility.Ctx(ctx).
		Where(dao.QuestionVisibility.Columns().QuestionId, srcQID).Scan(&vis); err != nil {
		return err
	}
	for _, v := range vis {
		remapped := remapCSVInt64(v.VisibleRoleIds, roleMap)
		if remapped == "" {
			continue
		}
		if _, err := dao.QuestionVisibility.Ctx(ctx).Data(do.QuestionVisibility{
			QuestionId: newQID, VisibleRoleIds: remapped,
		}).Insert(); err != nil {
			return err
		}
	}
	// 受限评价者名单：EmpID 为评价者全局身份、不随问卷变化，原样深拷贝无需重映射。
	var excluded []*entitymodel.QuestionExcludedEvaluator
	if err := dao.QuestionExcludedEvaluator.Ctx(ctx).
		Where(dao.QuestionExcludedEvaluator.Columns().QuestionId, srcQID).Scan(&excluded); err != nil {
		return err
	}
	if len(excluded) > 0 {
		rows := make([]do.QuestionExcludedEvaluator, 0, len(excluded))
		for _, e := range excluded {
			rows = append(rows, do.QuestionExcludedEvaluator{QuestionId: newQID, EvaluatorEmpId: e.EvaluatorEmpId})
		}
		if _, err := dao.QuestionExcludedEvaluator.Ctx(ctx).Data(rows).Insert(); err != nil {
			return err
		}
	}
	return nil
}

// remapCSVInt64 把逗号分隔的旧 id 列表按映射表重写为新 id 列表；无映射项丢弃。
func remapCSVInt64(csv string, idMap map[int64]int64) string {
	oldIDs := parseCSVInt64(csv)
	newIDs := make([]int64, 0, len(oldIDs))
	for _, oldID := range oldIDs {
		if nid, ok := idMap[oldID]; ok && nid > 0 {
			newIDs = append(newIDs, nid)
		}
	}
	return joinCSVInt64(newIDs)
}
