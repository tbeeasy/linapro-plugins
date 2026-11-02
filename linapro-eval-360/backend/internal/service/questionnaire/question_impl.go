// question_impl.go 实现题目的新增/就地编辑（连同选项、矩阵行列、可见性一体保存）
// 与删除。存在历史作答时，编辑按策略①白名单强校验：仅允许改文案/新增/调排序，
// 拒绝改分值、删题、删选项/列。
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
)

// parsedOption 是解析并校验分值后的单选选项。
type parsedOption struct {
	Id        int64
	Label     string
	Score     decimal.Decimal
	AllowText int
	Sort      int
}

// parsedColumn 是解析并校验分值后的矩阵列。
// 评语配置原样承载：AllowComment 为开关，CommentRequired 仅在开关开启时有意义
// （提交时校验），CommentPlaceholder 为输入框占位提示，CommentPresets 为预设候选，
// CommentAllowCustom 表示有预设时是否还允许自定义输入。
type parsedColumn struct {
	Id                 int64
	Label              string
	Score              decimal.Decimal
	Sort               int
	AllowComment       int
	CommentRequired    int
	CommentPlaceholder string
	CommentPresets     []string
	CommentAllowCustom int
}

// isValidQuestionType 判断题型是否受支持。
func isValidQuestionType(t string) bool {
	switch t {
	case QuestionTypeSingleChoice, QuestionTypeTextShort, QuestionTypeTextLong, QuestionTypeMatrix:
		return true
	default:
		return false
	}
}

// SaveQuestion 新增（Id=0）或就地编辑题目。
func (s *serviceImpl) SaveQuestion(ctx context.Context, in SaveQuestionInput) (int64, error) {
	if !isValidQuestionType(in.Type) {
		return 0, bizerr.NewCode(CodeInvalidQuestionType)
	}
	if _, err := s.mustGetQuestionnaire(ctx, in.QuestionnaireID); err != nil {
		return 0, err
	}
	// 每题必须挂一个属于本问卷的维度。
	if in.DimensionID <= 0 {
		return 0, bizerr.NewCode(CodeQuestionDimensionRequired)
	}
	dim, err := s.mustGetDimension(ctx, in.DimensionID)
	if err != nil {
		return 0, err
	}
	if dim.QuestionnaireId != in.QuestionnaireID {
		return 0, bizerr.NewCode(CodeQuestionDimensionRequired)
	}

	// 结构校验 + 分值解析。
	parsedOpts, parsedCols, err := parseAndValidateQuestion(in)
	if err != nil {
		return 0, err
	}

	restricted, err := s.editRestricted(ctx, in.QuestionnaireID)
	if err != nil {
		return 0, err
	}

	var existing *entitymodel.Question
	if in.Id > 0 {
		existing, err = s.mustGetQuestion(ctx, in.Id)
		if err != nil {
			return 0, err
		}
		if existing.QuestionnaireId != in.QuestionnaireID {
			return 0, bizerr.NewCode(CodeQuestionNotFound)
		}
		// 已发布或已有作答：策略①白名单强校验。
		if restricted {
			if err = s.guardWhitelistEdit(ctx, existing, in, parsedOpts, parsedCols); err != nil {
				return 0, err
			}
		}
	}

	var resultID int64
	err = dao.Question.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		if in.Id == 0 {
			id, insErr := dao.Question.Ctx(ctx).Data(do.Question{
				QuestionnaireId: in.QuestionnaireID,
				DimensionId:     in.DimensionID,
				Type:            in.Type,
				Title:           in.Title,
				Description:     in.Description,
				Required:        in.Required,
				Sort:            in.Sort,
			}).InsertAndGetId()
			if insErr != nil {
				return insErr
			}
			resultID = id
		} else {
			resultID = in.Id
			if _, upErr := dao.Question.Ctx(ctx).
				Where(dao.Question.Columns().Id, in.Id).
				Data(g.Map{
					dao.Question.Columns().DimensionId: in.DimensionID,
					dao.Question.Columns().Type:        in.Type,
					dao.Question.Columns().Title:       in.Title,
					dao.Question.Columns().Description: in.Description,
					dao.Question.Columns().Required:    in.Required,
					dao.Question.Columns().Sort:        in.Sort,
				}).Update(); upErr != nil {
				return upErr
			}
		}

		// 按题型协调从属结构；非适用结构一律清除（仅在无作答/新增时会真正删除）。
		if in.Type == QuestionTypeSingleChoice {
			if err := s.reconcileOptions(ctx, resultID, parsedOpts); err != nil {
				return err
			}
		} else if err := s.deleteChildTable(ctx, dao.QuestionOption.Ctx(ctx), dao.QuestionOption.Columns().QuestionId, resultID); err != nil {
			return err
		}

		if in.Type == QuestionTypeMatrix {
			// 矩阵不再手填行（行由被评估人清单动态渲染）：始终清空历史手填行，仅协调列。
			if err := s.deleteChildTable(ctx, dao.MatrixRow.Ctx(ctx), dao.MatrixRow.Columns().QuestionId, resultID); err != nil {
				return err
			}
			if err := s.reconcileMatrixColumns(ctx, resultID, parsedCols); err != nil {
				return err
			}
		} else {
			if err := s.deleteChildTable(ctx, dao.MatrixRow.Ctx(ctx), dao.MatrixRow.Columns().QuestionId, resultID); err != nil {
				return err
			}
			if err := s.deleteChildTable(ctx, dao.MatrixColumn.Ctx(ctx), dao.MatrixColumn.Columns().QuestionId, resultID); err != nil {
				return err
			}
		}

		// 可见性与受限评价者名单：整体替换（可见性变更不影响历史答案，任何时候允许，
		// 故不纳入策略①白名单拦截）。
		if err := s.replaceVisibility(ctx, resultID, in.VisibleRoleIDs); err != nil {
			return err
		}
		return s.replaceExcludedEvaluators(ctx, resultID, in.ExcludedEvaluatorEmpIDs)
	})
	if err != nil {
		return 0, err
	}
	return resultID, nil
}

// ReorderQuestions 按给定顺序重写某维度下全部题目的 sort（0 起连续）。
// 校验 QuestionIDs 恰好覆盖该维度当前全部题目（不重不漏、无跨维度 id），
// 随后在一个事务内整体重写 sort，避免逐条更新出现半顺延的中间态。
// 调排序属白名单允许的编辑，故存在作答时同样放行。
func (s *serviceImpl) ReorderQuestions(ctx context.Context, in ReorderQuestionsInput) error {
	if _, err := s.mustGetQuestionnaire(ctx, in.QuestionnaireID); err != nil {
		return err
	}
	dim, err := s.mustGetDimension(ctx, in.DimensionID)
	if err != nil {
		return err
	}
	if dim.QuestionnaireId != in.QuestionnaireID {
		return bizerr.NewCode(CodeQuestionDimensionRequired)
	}

	// 取该维度当前全部题目 id。
	var currentIDs []int64
	vals, err := dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().DimensionId, in.DimensionID).
		Fields(dao.Question.Columns().Id).Array()
	if err != nil {
		return err
	}
	currentIDs = make([]int64, 0, len(vals))
	for _, v := range vals {
		currentIDs = append(currentIDs, v.Int64())
	}

	// 入参必须与当前集合完全一致：数量相等 + 无重复 + 全部命中，防止漏题/串维度。
	if len(in.QuestionIDs) != len(currentIDs) {
		return bizerr.NewCode(CodeQuestionOrderMismatch)
	}
	expected := make(map[int64]struct{}, len(currentIDs))
	for _, id := range currentIDs {
		expected[id] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(in.QuestionIDs))
	for _, id := range in.QuestionIDs {
		if _, ok := expected[id]; !ok {
			return bizerr.NewCode(CodeQuestionOrderMismatch)
		}
		if _, dup := seen[id]; dup {
			return bizerr.NewCode(CodeQuestionOrderMismatch)
		}
		seen[id] = struct{}{}
	}

	return dao.Question.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		for sort, id := range in.QuestionIDs {
			if _, err := dao.Question.Ctx(ctx).
				Where(dao.Question.Columns().Id, id).
				Data(g.Map{dao.Question.Columns().Sort: sort}).Update(); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteQuestion 删除题目及其从属结构。已发布或存在作答时拒绝（引导改用删除答案/复制问卷）。
func (s *serviceImpl) DeleteQuestion(ctx context.Context, id int64) error {
	question, err := s.mustGetQuestion(ctx, id)
	if err != nil {
		return err
	}
	restricted, err := s.editRestricted(ctx, question.QuestionnaireId)
	if err != nil {
		return err
	}
	if restricted {
		return bizerr.NewCode(CodeEditRequiresStrategy)
	}
	return dao.Question.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		if err := s.deleteQuestionChildren(ctx, []int64{id}); err != nil {
			return err
		}
		_, err := dao.Question.Ctx(ctx).
			Where(dao.Question.Columns().Id, id).Delete()
		return err
	})
}

// mustGetQuestion 读取题目（租户过滤），不存在返回 CodeQuestionNotFound。
func (s *serviceImpl) mustGetQuestion(ctx context.Context, id int64) (*entitymodel.Question, error) {
	var qq *entitymodel.Question
	if err := dao.Question.Ctx(ctx).
		Where(dao.Question.Columns().Id, id).Scan(&qq); err != nil {
		return nil, err
	}
	if qq == nil {
		return nil, bizerr.NewCode(CodeQuestionNotFound)
	}
	return qq, nil
}

// parseAndValidateQuestion 校验题目结构并解析分值。
func parseAndValidateQuestion(in SaveQuestionInput) ([]parsedOption, []parsedColumn, error) {
	switch in.Type {
	case QuestionTypeSingleChoice:
		if len(in.Options) == 0 {
			return nil, nil, bizerr.NewCode(CodeSingleChoiceOptionsRequired)
		}
		opts := make([]parsedOption, 0, len(in.Options))
		for _, o := range in.Options {
			score, err := parseScore(o.Score)
			if err != nil {
				return nil, nil, err
			}
			opts = append(opts, parsedOption{
				Id: o.Id, Label: o.Label, Score: score,
				AllowText: normalizeFlag(o.AllowText), Sort: o.Sort,
			})
		}
		return opts, nil, nil
	case QuestionTypeMatrix:
		// 矩阵行改由被评估人清单动态渲染，不再手填；题目定义只需 ≥1 列（评价项）。
		if len(in.MatrixColumns) == 0 {
			return nil, nil, bizerr.NewCode(CodeMatrixColumnsRequired)
		}
		cols := make([]parsedColumn, 0, len(in.MatrixColumns))
		for _, c := range in.MatrixColumns {
			score, err := parseScore(c.Score)
			if err != nil {
				return nil, nil, err
			}
			cols = append(cols, parsedColumn{
				Id:                 c.Id,
				Label:              c.Label,
				Score:              score,
				Sort:               c.Sort,
				AllowComment:       normalizeFlag(c.AllowComment),
				CommentRequired:    normalizeFlag(c.CommentRequired),
				CommentPlaceholder: strings.TrimSpace(c.CommentPlaceholder),
				CommentPresets:     c.CommentPresets,
				CommentAllowCustom: normalizeFlag(c.CommentAllowCustom),
			})
		}
		return nil, cols, nil
	default:
		// 文本题：无选项/矩阵结构。
		return nil, nil, nil
	}
}

// parseScore 把分值字符串解析为 decimal，空串记 0，非法返回 CodeInvalidScore。
func parseScore(s string) (decimal.Decimal, error) {
	if s == "" {
		return decimal.Zero, nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, bizerr.NewCode(CodeInvalidScore)
	}
	return d, nil
}

// normalizeFlag 把任意非零值归一化为 1，其余为 0。
func normalizeFlag(v int) int {
	if v != 0 {
		return 1
	}
	return 0
}

// guardWhitelistEdit 在存在作答时执行策略①白名单校验：
// 禁止改题型、改分值、删选项/列/行。纯比较逻辑见 whitelist_check.go。
func (s *serviceImpl) guardWhitelistEdit(ctx context.Context, existing *entitymodel.Question, in SaveQuestionInput, parsedOpts []parsedOption, parsedCols []parsedColumn) error {
	if existing.Type != in.Type {
		return bizerr.NewCode(CodeEditRequiresStrategy)
	}
	switch in.Type {
	case QuestionTypeSingleChoice:
		var rows []*entitymodel.QuestionOption
		if err := dao.QuestionOption.Ctx(ctx).
			Where(dao.QuestionOption.Columns().QuestionId, existing.Id).Scan(&rows); err != nil {
			return err
		}
		return checkOptionWhitelist(rows, parsedOpts)
	case QuestionTypeMatrix:
		// 列：禁改分值、禁删列。（矩阵已无手填行，行由被评估人清单动态渲染，无需校验。）
		var cols []*entitymodel.MatrixColumn
		if err := dao.MatrixColumn.Ctx(ctx).
			Where(dao.MatrixColumn.Columns().QuestionId, existing.Id).Scan(&cols); err != nil {
			return err
		}
		return checkMatrixWhitelist(cols, parsedCols)
	}
	return nil
}

// reconcileOptions 协调单选选项：更新已有、插入新增、删除移除项。
func (s *serviceImpl) reconcileOptions(ctx context.Context, questionID int64, opts []parsedOption) error {
	var existingIDs []int64
	if err := s.collectChildIDs(ctx, dao.QuestionOption.Ctx(ctx), dao.QuestionOption.Columns().QuestionId, questionID, &existingIDs); err != nil {
		return err
	}
	keep := make(map[int64]struct{}, len(opts))
	for _, o := range opts {
		if o.Id > 0 {
			keep[o.Id] = struct{}{}
			score := o.Score
			if _, err := dao.QuestionOption.Ctx(ctx).
				Where(dao.QuestionOption.Columns().Id, o.Id).
				Data(do.QuestionOption{
					Label: o.Label, Score: &score, AllowText: o.AllowText, Sort: o.Sort,
				}).Update(); err != nil {
				return err
			}
			continue
		}
		score := o.Score
		if _, err := dao.QuestionOption.Ctx(ctx).Data(do.QuestionOption{
			QuestionId: questionID,
			Label:      o.Label, Score: &score, AllowText: o.AllowText, Sort: o.Sort,
		}).Insert(); err != nil {
			return err
		}
	}
	return s.deleteRemovedChildren(ctx, dao.QuestionOption.Ctx(ctx), existingIDs, keep)
}

// reconcileMatrixColumns 协调矩阵列：更新已有列、插入新增列、删除已被移除的列。
// 评语配置与文案/分值/排序一并写入，保证单列操作栏的改动随题目保存落库。
func (s *serviceImpl) reconcileMatrixColumns(ctx context.Context, questionID int64, cols []parsedColumn) error {
	var existingIDs []int64
	if err := s.collectChildIDs(ctx, dao.MatrixColumn.Ctx(ctx), dao.MatrixColumn.Columns().QuestionId, questionID, &existingIDs); err != nil {
		return err
	}
	keep := make(map[int64]struct{}, len(cols))
	for _, c := range cols {
		score := c.Score
		presets, err := encodeCommentPresets(c.CommentPresets)
		if err != nil {
			return err
		}
		if c.Id > 0 {
			keep[c.Id] = struct{}{}
			if _, err := dao.MatrixColumn.Ctx(ctx).
				Where(dao.MatrixColumn.Columns().Id, c.Id).
				Data(do.MatrixColumn{
					Label:              c.Label,
					Score:              &score,
					Sort:               c.Sort,
					AllowComment:       c.AllowComment,
					CommentRequired:    c.CommentRequired,
					CommentPlaceholder: c.CommentPlaceholder,
					CommentPresets:     presets,
					CommentAllowCustom: c.CommentAllowCustom,
				}).Update(); err != nil {
				return err
			}
			continue
		}
		if _, err := dao.MatrixColumn.Ctx(ctx).Data(do.MatrixColumn{
			QuestionId:         questionID,
			Label:              c.Label,
			Score:              &score,
			Sort:               c.Sort,
			AllowComment:       c.AllowComment,
			CommentRequired:    c.CommentRequired,
			CommentPlaceholder: c.CommentPlaceholder,
			CommentPresets:     presets,
			CommentAllowCustom: c.CommentAllowCustom,
		}).Insert(); err != nil {
			return err
		}
	}
	return s.deleteRemovedChildren(ctx, dao.MatrixColumn.Ctx(ctx), existingIDs, keep)
}

// replaceVisibility 整体替换题目的可见角色（空=全员，删除可见性行）。
func (s *serviceImpl) replaceVisibility(ctx context.Context, questionID int64, roleIDs []int64) error {
	if _, err := dao.QuestionVisibility.Ctx(ctx).
		Where(dao.QuestionVisibility.Columns().QuestionId, questionID).Delete(); err != nil {
		return err
	}
	csv := joinCSVInt64(roleIDs)
	if csv == "" {
		return nil // 空=全员可见，不落行。
	}
	_, err := dao.QuestionVisibility.Ctx(ctx).Data(do.QuestionVisibility{
		QuestionId: questionID, VisibleRoleIds: csv,
	}).Insert()
	return err
}

// replaceExcludedEvaluators 整体替换题目的受限评价者排除名单（按评价者 EmpID）。
// 名单对评价者全局生效、与被评估人无关；去零去重后一次批量写入。
func (s *serviceImpl) replaceExcludedEvaluators(ctx context.Context, questionID int64, evaluatorEmpIDs []int64) error {
	if _, err := dao.QuestionExcludedEvaluator.Ctx(ctx).
		Where(dao.QuestionExcludedEvaluator.Columns().QuestionId, questionID).Delete(); err != nil {
		return err
	}
	var (
		rows = make([]do.QuestionExcludedEvaluator, 0, len(evaluatorEmpIDs))
		seen = make(map[int64]struct{}, len(evaluatorEmpIDs))
	)
	for _, empID := range evaluatorEmpIDs {
		if empID <= 0 {
			continue
		}
		if _, ok := seen[empID]; ok {
			continue
		}
		seen[empID] = struct{}{}
		rows = append(rows, do.QuestionExcludedEvaluator{QuestionId: questionID, EvaluatorEmpId: empID})
	}
	if len(rows) == 0 {
		return nil // 空=不限制，不落行。
	}
	_, err := dao.QuestionExcludedEvaluator.Ctx(ctx).Data(rows).Insert()
	return err
}

// collectChildIDs 收集某题从属子表的全部 id。
func (s *serviceImpl) collectChildIDs(ctx context.Context, model *gdb.Model, col string, questionID int64, out *[]int64) error {
	vals, err := model.Where(col, questionID).Fields("id").Array()
	if err != nil {
		return err
	}
	ids := make([]int64, 0, len(vals))
	for _, v := range vals {
		ids = append(ids, v.Int64())
	}
	*out = ids
	return nil
}

// deleteRemovedChildren 删除不在 keep 集合内的从属子表行（按 id）。
func (s *serviceImpl) deleteRemovedChildren(ctx context.Context, model *gdb.Model, existingIDs []int64, keep map[int64]struct{}) error {
	toDelete := make([]int64, 0)
	for _, id := range existingIDs {
		if _, ok := keep[id]; !ok {
			toDelete = append(toDelete, id)
		}
	}
	if len(toDelete) == 0 {
		return nil
	}
	_, err := model.WhereIn("id", toDelete).Delete()
	return err
}

// deleteChildTable 清空某题在指定子表的全部行（题型切换后清理不适用结构）。
func (s *serviceImpl) deleteChildTable(ctx context.Context, model *gdb.Model, col string, questionID int64) error {
	_, err := model.Where(col, questionID).Delete()
	return err
}

// decimalEqual 比较两个 NUMERIC 值（existing 为 nil 记 0）。
func decimalEqual(existing *decimal.Decimal, val decimal.Decimal) bool {
	if existing == nil {
		return decimal.Zero.Equal(val)
	}
	return existing.Equal(val)
}
