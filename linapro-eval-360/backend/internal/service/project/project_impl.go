// project_impl.go 实现项目 CRUD、详情（跨问卷总完成率）、截止锁定派生与一键发送编排。
package project

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-eval-360/backend/internal/dao"
	"lina-plugin-linapro-eval-360/backend/internal/model/do"
	entitymodel "lina-plugin-linapro-eval-360/backend/internal/model/entity"
	"lina-plugin-linapro-eval-360/backend/internal/notify/notifycode"
	"lina-plugin-linapro-eval-360/backend/internal/service/questionnaire"
	"lina-plugin-linapro-eval-360/backend/internal/service/textutil"
)

// mustGetProject 读取项目（租户过滤），不存在返回 CodeProjectNotFound。
func (s *serviceImpl) mustGetProject(ctx context.Context, id int64) (*entitymodel.Project, error) {
	var p *entitymodel.Project
	if err := dao.Project.Ctx(ctx).
		Where(dao.Project.Columns().Id, id).Scan(&p); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, bizerr.NewCode(CodeProjectNotFound)
	}
	return p, nil
}

// List 返回当前租户下的全部项目（按 id 降序）。
func (s *serviceImpl) List(ctx context.Context) ([]*ProjectListItem, error) {
	var rows []*entitymodel.Project
	if err := dao.Project.Ctx(ctx).
		OrderDesc(dao.Project.Columns().Id).Scan(&rows); err != nil {
		return nil, err
	}
	items := make([]*ProjectListItem, 0, len(rows))
	for _, p := range rows {
		item := toListItem(p, s.deriveStatus(ctx, p))
		items = append(items, &item)
	}
	return items, nil
}

// GetDetail 返回项目详情（基本信息 + 问卷卡片 + 跨问卷总完成率）。
func (s *serviceImpl) GetDetail(ctx context.Context, id int64) (*ProjectDetail, error) {
	p, err := s.mustGetProject(ctx, id)
	if err != nil {
		return nil, err
	}

	// 问卷列表（经 questionnaire 服务的列表项结构）。
	qList, err := s.questionnaire.ListByProject(ctx, id)
	if err != nil {
		return nil, err
	}

	// 被评估人数与已填/应填关系数（按问卷聚合）。
	qIDs := make([]int64, 0, len(qList))
	for _, q := range qList {
		qIDs = append(qIDs, q.Id)
	}
	evalCountByQ, completedByQ, expectedByQ, err := s.aggregateRelationStats(ctx, qIDs)
	if err != nil {
		return nil, err
	}

	cards := make([]*QuestionnaireCard, 0, len(qList))
	var totalCompleted, totalExpected int
	for _, q := range qList {
		card := &QuestionnaireCard{
			Id: q.Id, Title: q.Title, Published: q.Published, Sort: q.Sort,
			EvaluateeCount: evalCountByQ[q.Id],
			Completed:      completedByQ[q.Id],
			Expected:       expectedByQ[q.Id],
			CreatedAt:      q.CreatedAt,
		}
		cards = append(cards, card)
		totalCompleted += card.Completed
		totalExpected += card.Expected
	}

	return &ProjectDetail{
		ProjectListItem: toListItem(p, s.deriveStatus(ctx, p)),
		Questionnaires:  cards,
		TotalCompleted:  totalCompleted,
		TotalExpected:   totalExpected,
	}, nil
}

// PageQuestionnaires 分页返回项目下的问卷卡片：先经问卷服务分页取当页问卷，
// 再按当页问卷 id 聚合被评估人数与已填/应填关系数，组装为含完成率的卡片。
func (s *serviceImpl) PageQuestionnaires(ctx context.Context, projectID int64, keyword string, pageNum, pageSize int) ([]*QuestionnaireCard, int, error) {
	// 校验项目存在（租户过滤），避免跨租户分页。
	if _, err := s.mustGetProject(ctx, projectID); err != nil {
		return nil, 0, err
	}

	qList, total, err := s.questionnaire.PageByProject(ctx, projectID, keyword, pageNum, pageSize)
	if err != nil {
		return nil, 0, err
	}

	qIDs := make([]int64, 0, len(qList))
	for _, q := range qList {
		qIDs = append(qIDs, q.Id)
	}
	evalCountByQ, completedByQ, expectedByQ, err := s.aggregateRelationStats(ctx, qIDs)
	if err != nil {
		return nil, 0, err
	}

	cards := make([]*QuestionnaireCard, 0, len(qList))
	for _, q := range qList {
		cards = append(cards, &QuestionnaireCard{
			Id: q.Id, Title: q.Title, Published: q.Published, Sort: q.Sort,
			EvaluateeCount: evalCountByQ[q.Id],
			Completed:      completedByQ[q.Id],
			Expected:       expectedByQ[q.Id],
			CreatedAt:      q.CreatedAt,
		})
	}
	return cards, total, nil
}

// aggregateRelationStats 按问卷聚合被评估人数与已填/应填关系数。
func (s *serviceImpl) aggregateRelationStats(ctx context.Context, qIDs []int64) (evalCount, completed, expected map[int64]int, err error) {
	evalCount = make(map[int64]int)
	completed = make(map[int64]int)
	expected = make(map[int64]int)
	if len(qIDs) == 0 {
		return evalCount, completed, expected, nil
	}
	// 被评估人数。
	var evalRows []struct {
		QuestionnaireId int64 `orm:"questionnaire_id"`
		Cnt             int   `orm:"cnt"`
	}
	if err = dao.Evaluatee.Ctx(ctx).
		WhereIn(dao.Evaluatee.Columns().QuestionnaireId, qIDs).
		Fields("questionnaire_id, count(*) as cnt").
		Group("questionnaire_id").Scan(&evalRows); err != nil {
		return evalCount, completed, expected, err
	}
	for _, r := range evalRows {
		evalCount[r.QuestionnaireId] = r.Cnt
	}
	// 关系数（已填 / 应填）。
	var relRows []struct {
		QuestionnaireId int64 `orm:"questionnaire_id"`
		Completed       int   `orm:"completed"`
		Cnt             int   `orm:"cnt"`
	}
	if err = dao.Relation.Ctx(ctx).
		WhereIn(dao.Relation.Columns().QuestionnaireId, qIDs).
		Fields("questionnaire_id, completed, count(*) as cnt").
		Group("questionnaire_id, completed").Scan(&relRows); err != nil {
		return evalCount, completed, expected, err
	}
	for _, r := range relRows {
		expected[r.QuestionnaireId] += r.Cnt
		if r.Completed == 1 {
			completed[r.QuestionnaireId] += r.Cnt
		}
	}
	return evalCount, completed, expected, nil
}

// Create 新建项目。状态派生为草稿（无已发布问卷）。
// 名称先归一化，在全平台范围内查重；撞名或纯空白名称直接拒绝。
func (s *serviceImpl) Create(ctx context.Context, in CreateInput) (int64, error) {
	name := textutil.NormalizeName(in.Name)
	if name == "" {
		return 0, bizerr.NewCode(CodeProjectNameEmpty)
	}
	dup, err := s.projectNameExists(ctx, name, 0)
	if err != nil {
		return 0, err
	}
	if dup {
		return 0, bizerr.NewCode(CodeProjectNameExists)
	}
	id, err := dao.Project.Ctx(ctx).Data(do.Project{
		Name:        name,
		Description: in.Description,
		StartAt:     in.StartAt,
		EndAt:       in.EndAt,
		CreatedBy:   in.CreatedBy,
	}).InsertAndGetId()
	if err != nil {
		// 并发穿透：预检与写入之间的竞态由唯一索引兜底。
		if isUniqueViolation(err) {
			return 0, bizerr.NewCode(CodeProjectNameExists)
		}
		return 0, err
	}
	return id, nil
}

// Update 就地修改项目元信息。改名时全平台查重（排除自身）。
func (s *serviceImpl) Update(ctx context.Context, in UpdateInput) error {
	p, err := s.mustGetProject(ctx, in.Id)
	if err != nil {
		return err
	}
	data := g.Map{}
	if in.Name != nil {
		name := textutil.NormalizeName(*in.Name)
		if name == "" {
			return bizerr.NewCode(CodeProjectNameEmpty)
		}
		// 与当前名称一致 → 幂等放行，不做查重（否则会与自身撞名）。
		if name != p.Name {
			dup, dErr := s.projectNameExists(ctx, name, p.Id)
			if dErr != nil {
				return dErr
			}
			if dup {
				return bizerr.NewCode(CodeProjectNameExists)
			}
		}
		data[dao.Project.Columns().Name] = name
	}
	if in.Description != nil {
		data[dao.Project.Columns().Description] = *in.Description
	}
	if in.StartAt != nil {
		data[dao.Project.Columns().StartAt] = *in.StartAt
	}
	if in.EndAt != nil {
		data[dao.Project.Columns().EndAt] = *in.EndAt
	}
	if len(data) == 0 {
		return nil
	}
	_, err = dao.Project.Ctx(ctx).
		Where(dao.Project.Columns().Id, in.Id).
		Data(data).Update()
	if err != nil && isUniqueViolation(err) {
		return bizerr.NewCode(CodeProjectNameExists)
	}
	return err
}

// Delete 删除项目。存在已发布问卷或作答时拒绝，引导先处理。
func (s *serviceImpl) Delete(ctx context.Context, id int64) error {
	if _, err := s.mustGetProject(ctx, id); err != nil {
		return err
	}
	// 存在已发布问卷 → 拒绝（需先取消发布）。
	cnt, err := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().ProjectId, id).
		Where(dao.Questionnaire.Columns().Published, questionnaire.PublishedYes).Count()
	if err != nil {
		return err
	}
	if cnt > 0 {
		return bizerr.NewCode(CodeProjectHasPublishedQuestionnaire)
	}
	// 存在作答 → 拒绝（需先清空作答）。
	subCnt, err := dao.Submission.Ctx(ctx).
		WhereIn(dao.Submission.Columns().QuestionnaireId, func() []int64 {
			qs, _ := s.questionnaire.ListByProject(ctx, id)
			ids := make([]int64, 0, len(qs))
			for _, q := range qs {
				ids = append(ids, q.Id)
			}
			return ids
		}()).Count()
	if err != nil {
		return err
	}
	if subCnt > 0 {
		return bizerr.NewCode(CodeProjectHasSubmissions)
	}
	return dao.Project.Transaction(ctx, func(ctx context.Context, _ gdb.TX) error {
		// 级联清理问卷结构由 questionnaire.DeleteQuestionnaire 逐份处理；此处只删项目本体
		// 与残留的空关系/被评估人（理论上已无作答、无已发布问卷）。
		if _, err := dao.Relation.Ctx(ctx).
			Where(dao.Relation.Columns().ProjectId, id).Delete(); err != nil {
			return err
		}
		if _, err := dao.Evaluatee.Ctx(ctx).
			Where(dao.Evaluatee.Columns().ProjectId, id).Delete(); err != nil {
			return err
		}
		_, err := dao.Project.Ctx(ctx).
			Where(dao.Project.Columns().Id, id).Delete()
		return err
	})
}

// Close 提前结束项目：置 closed_at 为当前时间，不改 end_at。
func (s *serviceImpl) Close(ctx context.Context, id int64) error {
	if _, err := s.mustGetProject(ctx, id); err != nil {
		return err
	}
	now := time.Now()
	_, err := dao.Project.Ctx(ctx).
		Where(dao.Project.Columns().Id, id).
		Data(map[string]any{dao.Project.Columns().ClosedAt: now}).Update()
	return err
}

// Reopen 重新开启已提前结束的项目：清空 closed_at。
func (s *serviceImpl) Reopen(ctx context.Context, id int64) error {
	if _, err := s.mustGetProject(ctx, id); err != nil {
		return err
	}
	_, err := dao.Project.Ctx(ctx).
		Where(dao.Project.Columns().Id, id).
		Data(map[string]any{dao.Project.Columns().ClosedAt: nil}).Update()
	return err
}

// IsClosed 读时派生项目截止状态：now > end_at 或 closed_at != nil。
func (s *serviceImpl) IsClosed(ctx context.Context, projectID int64) (bool, error) {
	p, err := s.mustGetProject(ctx, projectID)
	if err != nil {
		return false, err
	}
	return isProjectClosed(p, time.Now()), nil
}

// SendAll 一键发送：整批校验未发布问卷 → 推送飞书卡片 → 按问卷判定发布（已发布跳过、幂等）。
// 发送前置校验：项目内不得存在无题目的问卷；发布前校验角色权重（见 questionnaire 模块）。
// 校验任一失败拒绝整个请求；推送阶段按问卷部分发布，失败问卷随结果回报、不返回 error。
func (s *serviceImpl) SendAll(ctx context.Context, projectID int64) (*SendResult, error) {
	p, err := s.mustGetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	// 前置校验：项目内不得存在无题目的问卷。
	var qs []*entitymodel.Questionnaire
	if err = dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().ProjectId, projectID).
		OrderAsc(dao.Questionnaire.Columns().Sort).Scan(&qs); err != nil {
		return nil, err
	}
	emptyQ := make([]int64, 0)
	titles := make(map[int64]string, len(qs))
	for _, q := range qs {
		titles[q.Id] = q.Title
		qCnt, cErr := dao.Question.Ctx(ctx).
			Where(dao.Question.Columns().QuestionnaireId, q.Id).Count()
		if cErr != nil {
			return nil, cErr
		}
		if qCnt == 0 {
			emptyQ = append(emptyQ, q.Id)
		}
	}
	if len(emptyQ) > 0 {
		return nil, bizerr.NewCode(CodeProjectHasEmptyQuestionnaire)
	}
	// 推送配置前置校验：配置缺失时在校验与推送之前直接拦截。
	if s.sender != nil {
		if err = s.sender.Precheck(ctx); err != nil {
			return nil, err
		}
	}

	// 校验未发布问卷（已发布跳过、幂等）。questionnaire 服务内部做了发布前权重校验。
	ids, err := s.questionnaire.ValidateProjectPublish(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.publishEach(ctx, p.Id, ids, titles)
}

// publishEach 是一键发送校验通过后的编排（不直接访问数据库，便于以桩单测）：推送后只发布
// 评价者全部送达的问卷，失败问卷按待发布顺序带上问卷名、原因与失败工号回报。titles 为问卷
// id → 问卷名；请求级推送错误原样返回、不发布任何问卷。
func (s *serviceImpl) publishEach(ctx context.Context, projectID int64, ids []int64, titles map[int64]string) (*SendResult, error) {
	published, outcome, err := s.pushThenPublish(ctx, projectID, ids)
	if err != nil {
		return nil, err
	}
	failed := make([]*SendFailure, 0)
	for _, id := range ids {
		reason, labels, ok := outcome.failed(id)
		if !ok {
			continue
		}
		failed = append(failed, &SendFailure{
			QuestionnaireID: id,
			Title:           titles[id],
			Reason:          reason,
			EmployeeNos:     labels,
		})
	}
	return &SendResult{PublishedIDs: published, Failed: failed}, nil
}

// PublishQuestionnaire 发布单份问卷 + 推送飞书卡片：该问卷评价者全部送达才发布，
// 否则把推送结果转为业务错误返回（问卷保持未发布）。已发布问卷幂等返回空结果、不重推。
func (s *serviceImpl) PublishQuestionnaire(ctx context.Context, questionnaireID int64) (*SendResult, error) {
	if s.sender != nil {
		if err := s.sender.Precheck(ctx); err != nil {
			return nil, err
		}
	}
	projectID, needPublish, err := s.questionnaire.ValidatePublish(ctx, questionnaireID)
	if err != nil {
		return nil, err
	}
	if !needPublish {
		return &SendResult{PublishedIDs: []int64{}, Failed: []*SendFailure{}}, nil
	}
	published, outcome, err := s.pushThenPublish(ctx, projectID, []int64{questionnaireID})
	if err != nil {
		return nil, err
	}
	if reason, labels, ok := outcome.failed(questionnaireID); ok {
		return nil, failReasonError(reason, labels)
	}
	return &SendResult{PublishedIDs: published, Failed: []*SendFailure{}}, nil
}

// failReasonError 把单份问卷的推送失败转为对外业务错误（沿用 notify 的错误码与 count / list 参数）。
func failReasonError(reason FailReason, labels []string) error {
	if reason == FailReasonUnreachable {
		return bizerr.NewCode(notifycode.CodeEvaluatorsUnreachable, notifycode.FailedParams(labels)...)
	}
	return bizerr.NewCode(notifycode.CodePushFailed, notifycode.FailedParams(labels)...)
}

// pushThenPublish 先向待发布问卷的评价者推送卡片，再一次性只把推送通过的问卷写为已发布，
// 返回已发布的问卷 id（保持 ids 顺序）与按问卷的推送失败结果。
//
// 顺序是刻意的：若先发布再推送、失败后回滚，推送期间已收到卡片的评价者可能已开始作答，
// 回滚会留下「未发布问卷却有作答」的脏数据。先推送的代价只是卡片到达与发布落库之间的
// 短暂窗口（此时点开待办为空，刷新即可）。请求级推送错误原样返回，不写任何发布状态。
func (s *serviceImpl) pushThenPublish(ctx context.Context, projectID int64, ids []int64) ([]int64, PushOutcome, error) {
	published := make([]int64, 0, len(ids))
	if len(ids) == 0 {
		return published, PushOutcome{}, nil
	}
	var outcome PushOutcome
	if s.sender != nil {
		var err error
		if outcome, err = s.sender.SendAll(ctx, projectID, ids); err != nil {
			return nil, PushOutcome{}, err
		}
	}
	for _, id := range ids {
		if _, _, failed := outcome.failed(id); !failed {
			published = append(published, id)
		}
	}
	if len(published) == 0 {
		return published, outcome, nil
	}
	if err := s.questionnaire.MarkPublished(ctx, published); err != nil {
		return nil, PushOutcome{}, err
	}
	return published, outcome, nil
}

// failed 返回问卷在本次推送中的失败原因与失败评价者标签；问卷通过时 ok=false。
func (o PushOutcome) failed(questionnaireID int64) (reason FailReason, labels []string, ok bool) {
	if labels, ok = o.Unreachable[questionnaireID]; ok {
		return FailReasonUnreachable, labels, true
	}
	if labels, ok = o.Failed[questionnaireID]; ok {
		return FailReasonPushFailed, labels, true
	}
	return "", nil, false
}

// deriveStatus 派生项目状态：草稿/进行中/已结束。
// 无任何已发布问卷 = 草稿；有已发布问卷且未结束 = 进行中；已结束（派生）= closed。
func (s *serviceImpl) deriveStatus(ctx context.Context, p *entitymodel.Project) string {
	if isProjectClosed(p, time.Now()) {
		return StatusClosed
	}
	// 是否存在已发布问卷。
	cnt, err := dao.Questionnaire.Ctx(ctx).
		Where(dao.Questionnaire.Columns().ProjectId, p.Id).
		Where(dao.Questionnaire.Columns().Published, questionnaire.PublishedYes).Count()
	if err != nil {
		return StatusDraft
	}
	if cnt == 0 {
		return StatusDraft
	}
	return StatusActive
}

// isProjectClosed 判定项目是否已截止（读时派生，不落库、无 cron）。
func isProjectClosed(p *entitymodel.Project, now time.Time) bool {
	if p == nil {
		return false
	}
	if p.ClosedAt != nil {
		return true
	}
	if p.EndAt == nil {
		return false
	}
	return p.EndAt.Before(now)
}

// toListItem 把实体转为列表项（含派生状态）。
func toListItem(p *entitymodel.Project, status string) ProjectListItem {
	return ProjectListItem{
		Id:          p.Id,
		Name:        p.Name,
		Description: p.Description,
		Status:      status,
		StartAt:     p.StartAt,
		EndAt:       p.EndAt,
		ClosedAt:    p.ClosedAt,
	}
}
