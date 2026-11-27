// group.go 实现考勤组的列表、详情与成员查询，把官方 SDK 的响应投影成本库的 GroupMeta /
// GroupDetail / user_id 列表。列表与成员自动翻页；详情与成员固定传 dept_type=open_id
// （传 od_id 会返回 99992402）。
package larkattend

import (
	"context"

	"github.com/gogf/gf/v2/errors/gerror"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larksdk "github.com/larksuite/oapi-sdk-go/v3/service/attendance/v1"
)

const (
	// groupPageSize 是考勤组列表单页条数，取飞书允许的较大值减少往返。
	groupPageSize = 100
	// groupMemberPageSize 是考勤组成员单页条数。飞书文档未写明上限，取保守值，多翻几页不影响结果。
	groupMemberPageSize = 50
	// deptTypeOpenID 是「按 ID 查询考勤组」必须使用的部门 ID 类型，不开放给调用方覆盖。
	deptTypeOpenID = "open_id"
)

// MemberClockType 是查询考勤组成员时的打卡类型筛选，取值与飞书 member_clock_type 一致。
type MemberClockType int

const (
	// MemberClockTypeAll 表示全部成员。
	MemberClockTypeAll MemberClockType = 0
	// MemberClockTypeNeedPunch 表示需要打卡的成员。考勤组详情的 need_punch_members
	// 不代表需打卡名单（已实测），需打卡名单以本筛选为准。
	MemberClockTypeNeedPunch MemberClockType = 1
	// MemberClockTypeNoPunch 表示无需打卡的成员。
	MemberClockTypeNoPunch MemberClockType = 2
)

// ListGroups 返回该应用下全部考勤组的 id 与名称（自动翻页）。
func (c *Client) ListGroups(ctx context.Context) ([]GroupMeta, error) {
	groups := make([]GroupMeta, 0, groupPageSize)
	pageToken := ""
	for {
		reqBuilder := larksdk.NewListGroupReqBuilder().PageSize(groupPageSize)
		if pageToken != "" {
			reqBuilder.PageToken(pageToken)
		}
		req := reqBuilder.Build()
		var resp *larksdk.ListGroupResp
		if err := callWithBackoff(ctx, c.sleepOrDefault(), "attendance.groups.list",
			func() (*larkcore.ApiResp, larkcore.CodeError, error) {
				r, callErr := c.groups.List(ctx, req)
				resp = r
				if r == nil {
					return nil, larkcore.CodeError{}, callErr
				}
				return r.ApiResp, r.CodeError, callErr
			}); err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil {
			return groups, nil
		}
		for _, item := range resp.Data.GroupList {
			if item == nil || stringValue(item.GroupId) == "" {
				continue
			}
			groups = append(groups, GroupMeta{
				GroupID:   stringValue(item.GroupId),
				GroupName: stringValue(item.GroupName),
			})
		}
		if !boolValue(resp.Data.HasMore) {
			return groups, nil
		}
		pageToken = stringValue(resp.Data.PageToken)
		if pageToken == "" {
			return groups, nil
		}
	}
}

// GetGroup 按 ID 查询考勤组详情。
func (c *Client) GetGroup(ctx context.Context, groupID string) (*GroupDetail, error) {
	if groupID == "" {
		return nil, ErrInvalidArgument
	}
	req := larksdk.NewGetGroupReqBuilder().
		GroupId(groupID).
		DeptType(deptTypeOpenID).
		Build()
	var resp *larksdk.GetGroupResp
	if err := callWithBackoff(ctx, c.sleepOrDefault(), "attendance.groups.get",
		func() (*larkcore.ApiResp, larkcore.CodeError, error) {
			r, callErr := c.groups.Get(ctx, req)
			resp = r
			if r == nil {
				return nil, larkcore.CodeError{}, callErr
			}
			return r.ApiResp, r.CodeError, callErr
		}); err != nil {
		return nil, err
	}
	if resp == nil || resp.Data == nil {
		return nil, gerror.New("larkattend: 查询考勤组详情响应缺少 data")
	}
	return convertGroupDetail(resp.Data), nil
}

// ListGroupMembers 查询考勤组成员（自动翻页），返回飞书 user_id 列表。
//
// 固定传 employee_type=employee_id（与按人查询接口同一口径，返回值可直接喂给 QueryUserTasks 等）
// 与 dept_type=open_id；clockType 筛选打卡类型，预建等场景传 MemberClockTypeNeedPunch。
// 任一页调用失败即整次失败，不返回半份名单。
func (c *Client) ListGroupMembers(ctx context.Context, groupID string, clockType MemberClockType) ([]string, error) {
	if groupID == "" {
		return nil, ErrInvalidArgument
	}
	switch clockType {
	case MemberClockTypeAll, MemberClockTypeNeedPunch, MemberClockTypeNoPunch:
	default:
		return nil, ErrInvalidArgument
	}
	userIDs := make([]string, 0, groupMemberPageSize)
	pageToken := ""
	for {
		reqBuilder := larksdk.NewListUserGroupReqBuilder().
			GroupId(groupID).
			EmployeeType(userEmployeeType).
			DeptType(deptTypeOpenID).
			MemberClockType(int(clockType)).
			PageSize(groupMemberPageSize)
		if pageToken != "" {
			reqBuilder.PageToken(pageToken)
		}
		req := reqBuilder.Build()
		var resp *larksdk.ListUserGroupResp
		if err := callWithBackoff(ctx, c.sleepOrDefault(), "attendance.groups.list_user",
			func() (*larkcore.ApiResp, larkcore.CodeError, error) {
				r, callErr := c.groups.ListUser(ctx, req)
				resp = r
				if r == nil {
					return nil, larkcore.CodeError{}, callErr
				}
				return r.ApiResp, r.CodeError, callErr
			}); err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil {
			return userIDs, nil
		}
		for _, user := range resp.Data.Users {
			if user == nil || stringValue(user.UserId) == "" {
				continue
			}
			userIDs = append(userIDs, stringValue(user.UserId))
		}
		if !boolValue(resp.Data.HasMore) {
			return userIDs, nil
		}
		pageToken = stringValue(resp.Data.PageToken)
		if pageToken == "" {
			return userIDs, nil
		}
	}
}

// convertGroupDetail 把 SDK 的考勤组详情投影为本包的 GroupDetail。
func convertGroupDetail(data *larksdk.GetGroupRespData) *GroupDetail {
	return &GroupDetail{
		GroupID:                  stringValue(data.GroupId),
		GroupName:                stringValue(data.GroupName),
		GroupType:                intValue(data.GroupType),
		PunchDayShiftIDs:         append([]string(nil), data.PunchDayShiftIds...),
		AllowRemedy:              boolValue(data.AllowRemedy),
		RemedyLimitCount:         intValue(data.RemedyLimitCount),
		RemedyDateNum:            intValue(data.RemedyDateNum),
		AllowOutPunch:            boolValue(data.AllowOutPunch),
		OutPunchNeedPostApproval: boolValue(data.OutPunchNeedPostApproval),
		// 需打卡成员数取新口径 need_punch_members；飞书同时保留旧的 bind_user_ids，
		// 两者同时出现时以新字段为准（与飞书文档一致）。
		NeedPunchMemberCount: len(data.NeedPunchMembers),
	}
}
