// publish.go 实现发布状态与编辑策略（复制/清空作答）控制器方法。
package evalcontroller

import (
	"context"

	v1 "lina-plugin-linapro-eval-360/backend/api/eval/v1"
)

// PublishQuestionnaire 发布单份问卷并推送飞书卡片。
func (c *ControllerV1) PublishQuestionnaire(ctx context.Context, req *v1.PublishQuestionnaireReq) (res *v1.PublishQuestionnaireRes, err error) {
	result, err := c.projectSvc.PublishQuestionnaire(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return &v1.PublishQuestionnaireRes{PublishedIDs: result.PublishedIDs}, nil
}

// UnpublishQuestionnaire 取消发布单份问卷。
func (c *ControllerV1) UnpublishQuestionnaire(ctx context.Context, req *v1.UnpublishQuestionnaireReq) (res *v1.UnpublishQuestionnaireRes, err error) {
	if err = c.questionnaireSvc.Unpublish(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.UnpublishQuestionnaireRes{}, nil
}

// PublishProject 发布项目下全部未发布问卷：与一键发送同一编排（按问卷判定，评价者全部送达的
// 问卷才发布），不得绕过推送直接翻转发布状态。
func (c *ControllerV1) PublishProject(ctx context.Context, req *v1.PublishProjectReq) (res *v1.PublishProjectRes, err error) {
	result, err := c.projectSvc.SendAll(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	return &v1.PublishProjectRes{PublishedIds: result.PublishedIDs, Failed: toSendFailedDTOs(result.Failed)}, nil
}

// CopyQuestionnaire 复制问卷（策略③）。
func (c *ControllerV1) CopyQuestionnaire(ctx context.Context, req *v1.CopyQuestionnaireReq) (res *v1.CopyQuestionnaireRes, err error) {
	id, err := c.questionnaireSvc.CopyQuestionnaire(ctx, req.Id, req.Title)
	if err != nil {
		return nil, err
	}
	return &v1.CopyQuestionnaireRes{Id: id}, nil
}

// ClearAnswers 清空问卷作答（策略②）。
func (c *ControllerV1) ClearAnswers(ctx context.Context, req *v1.ClearAnswersReq) (res *v1.ClearAnswersRes, err error) {
	if err = c.questionnaireSvc.ClearAnswers(ctx, req.Id); err != nil {
		return nil, err
	}
	return &v1.ClearAnswersRes{}, nil
}
