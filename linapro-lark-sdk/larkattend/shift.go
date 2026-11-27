// shift.go 实现班次查询，把官方 SDK 的班次定义投影成本库的 Shift（含打卡规则、
// 休息段与晚走晚到规则）。班次列表自动翻页。
package larkattend

import (
	"context"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larksdk "github.com/larksuite/oapi-sdk-go/v3/service/attendance/v1"
)

const (
	// shiftPageSize 是班次列表单页条数，取飞书允许的较大值减少往返。
	shiftPageSize = 100
)

// ListShifts 返回该应用下全部班次（自动翻页）。
func (c *Client) ListShifts(ctx context.Context) ([]Shift, error) {
	shifts := make([]Shift, 0, shiftPageSize)
	pageToken := ""
	for {
		reqBuilder := larksdk.NewListShiftReqBuilder().PageSize(shiftPageSize)
		if pageToken != "" {
			reqBuilder.PageToken(pageToken)
		}
		req := reqBuilder.Build()
		var resp *larksdk.ListShiftResp
		if err := callWithBackoff(ctx, c.sleepOrDefault(), "attendance.shifts.list",
			func() (*larkcore.ApiResp, larkcore.CodeError, error) {
				r, callErr := c.shifts.List(ctx, req)
				resp = r
				if r == nil {
					return nil, larkcore.CodeError{}, callErr
				}
				return r.ApiResp, r.CodeError, callErr
			}); err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil {
			return shifts, nil
		}
		for _, item := range resp.Data.ShiftList {
			if item == nil || stringValue(item.ShiftId) == "" {
				continue
			}
			shifts = append(shifts, convertShift(item))
		}
		if !boolValue(resp.Data.HasMore) {
			return shifts, nil
		}
		pageToken = stringValue(resp.Data.PageToken)
		if pageToken == "" {
			return shifts, nil
		}
	}
}

// convertShift 把 SDK 的班次投影为本包的 Shift。
func convertShift(item *larksdk.Shift) Shift {
	shift := Shift{
		ShiftID:            stringValue(item.ShiftId),
		ShiftName:          stringValue(item.ShiftName),
		IsFlexible:         boolValue(item.IsFlexible),
		FlexibleMinutes:    intValue(item.FlexibleMinutes),
		PunchTimeRules:     make([]PunchTimeRule, 0, len(item.PunchTimeRule)),
		RestRules:          make([]RestRule, 0, len(item.RestTimeRule)),
		LateOffLateOnRules: make([]LateOffLateOnRule, 0, len(item.LateOffLateOnRule)),
	}
	for _, rule := range item.PunchTimeRule {
		if rule == nil {
			continue
		}
		shift.PunchTimeRules = append(shift.PunchTimeRules, PunchTimeRule{
			OnTime:                   stringValue(rule.OnTime),
			OffTime:                  stringValue(rule.OffTime),
			LateMinutesAsLate:        intValue(rule.LateMinutesAsLate),
			LateMinutesAsLack:        intValue(rule.LateMinutesAsLack),
			LateMinutesAsSeriousLate: intValue(rule.LateMinutesAsSeriousLate),
			EarlyMinutesAsEarly:      intValue(rule.EarlyMinutesAsEarly),
			EarlyMinutesAsLack:       intValue(rule.EarlyMinutesAsLack),
			OnAdvanceMinutes:         intValue(rule.OnAdvanceMinutes),
			OffDelayMinutes:          intValue(rule.OffDelayMinutes),
		})
	}
	for _, rule := range item.RestTimeRule {
		if rule == nil {
			continue
		}
		shift.RestRules = append(shift.RestRules, RestRule{
			BeginTime: stringValue(rule.RestBeginTime),
			EndTime:   stringValue(rule.RestEndTime),
		})
	}
	for _, rule := range item.LateOffLateOnRule {
		if rule == nil {
			continue
		}
		shift.LateOffLateOnRules = append(shift.LateOffLateOnRules, LateOffLateOnRule{
			LateOffMinutes: intValue(rule.LateOffMinutes),
			LateOnMinutes:  intValue(rule.LateOnMinutes),
		})
	}
	return shift
}
