// user.go 实现按人批量查询：打卡结果（user_tasks）、排班（user_daily_shifts）
// 与打卡流水（user_flows）。三个接口单次最多 50 人，统一按 batchSize 切片后串行调用，
// 任一批失败即整次失败，不返回半份结果。
package larkattend

import (
	"context"
	"strconv"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larksdk "github.com/larksuite/oapi-sdk-go/v3/service/attendance/v1"
)

// userEmployeeType 是按人查询（打卡结果、排班、打卡流水）与考勤组成员查询共用的 employee_type：userIDs 一律是飞书 user_id
// （S6 口径）。该参数在飞书侧为必填，SDK builder 没有默认值，各接口都必须显式传。
const userEmployeeType = "employee_id"

// QueryUserTasks 按人批量查询打卡结果。
//
// userIDs 是飞书 user_id（employee_type=employee_id）；dateFrom / dateTo 是 yyyyMMdd 整数
// （例如 20261118）。请求带 ignore_invalid_users=true：批内个别人员无效或无权限时不让整批失败，
// 这些人列在返回值的 InvalidUserIDs / UnauthorizedUserIDs 中。任一批调用失败即整次失败，不返回半份结果。
func (c *Client) QueryUserTasks(ctx context.Context, userIDs []string, dateFrom, dateTo int) (*UserTaskQueryResult, error) {
	result := &UserTaskQueryResult{Tasks: make([]UserTask, 0, len(userIDs))}
	for _, batch := range splitBatches(userIDs) {
		body := &larksdk.QueryUserTaskReqBody{
			UserIds:       batch,
			CheckDateFrom: &dateFrom,
			CheckDateTo:   &dateTo,
		}
		req := larksdk.NewQueryUserTaskReqBuilder().
			EmployeeType(userEmployeeType).
			IgnoreInvalidUsers(true).
			Body(body).
			Build()
		var resp *larksdk.QueryUserTaskResp
		if err := callWithBackoff(ctx, c.sleepOrDefault(), "attendance.user_tasks.query",
			func() (*larkcore.ApiResp, larkcore.CodeError, error) {
				r, callErr := c.userTasks.Query(ctx, req)
				resp = r
				if r == nil {
					return nil, larkcore.CodeError{}, callErr
				}
				return r.ApiResp, r.CodeError, callErr
			}); err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil {
			continue
		}
		for _, task := range resp.Data.UserTaskResults {
			if task == nil {
				continue
			}
			result.Tasks = append(result.Tasks, convertUserTask(task))
		}
		result.InvalidUserIDs = append(result.InvalidUserIDs, resp.Data.InvalidUserIds...)
		result.UnauthorizedUserIDs = append(result.UnauthorizedUserIDs, resp.Data.UnauthorizedUserIds...)
	}
	return result, nil
}

// QueryUserDailyShifts 按人批量查询排班，人员与日期参数语义同 QueryUserTasks。
//
// SDK v3.11.0 的该接口 builder 不支持 ignore_invalid_users，批内有无效或无权限人员时由飞书决定整批报错，
// 不返回剔除名单。
func (c *Client) QueryUserDailyShifts(ctx context.Context, userIDs []string, dateFrom, dateTo int) ([]UserDailyShift, error) {
	shifts := make([]UserDailyShift, 0, len(userIDs))
	for _, batch := range splitBatches(userIDs) {
		body := &larksdk.QueryUserDailyShiftReqBody{
			UserIds:       batch,
			CheckDateFrom: &dateFrom,
			CheckDateTo:   &dateTo,
		}
		req := larksdk.NewQueryUserDailyShiftReqBuilder().
			EmployeeType(userEmployeeType).
			Body(body).
			Build()
		var resp *larksdk.QueryUserDailyShiftResp
		if err := callWithBackoff(ctx, c.sleepOrDefault(), "attendance.user_daily_shifts.query",
			func() (*larkcore.ApiResp, larkcore.CodeError, error) {
				r, callErr := c.userDailyShifts.Query(ctx, req)
				resp = r
				if r == nil {
					return nil, larkcore.CodeError{}, callErr
				}
				return r.ApiResp, r.CodeError, callErr
			}); err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil {
			continue
		}
		for _, item := range resp.Data.UserDailyShifts {
			if item == nil {
				continue
			}
			shifts = append(shifts, UserDailyShift{
				UserID:  stringValue(item.UserId),
				GroupID: stringValue(item.GroupId),
				ShiftID: stringValue(item.ShiftId),
				Month:   intValue(item.Month),
				DayNo:   intValue(item.DayNo),
			})
		}
	}
	return shifts, nil
}

// QueryUserFlows 按人批量查询打卡流水。userIDs 是飞书 user_id（employee_type=employee_id）；
// [checkTimeFrom, checkTimeTo) 为左闭右开区间，内部转换为秒级时间戳字符串传给飞书。
//
// SDK v3.11.0 的该接口 builder 不支持 ignore_invalid_users，行为同 QueryUserDailyShifts。
func (c *Client) QueryUserFlows(ctx context.Context, userIDs []string, checkTimeFrom, checkTimeTo time.Time) ([]UserFlow, error) {
	from := strconv.FormatInt(checkTimeFrom.Unix(), 10)
	to := strconv.FormatInt(checkTimeTo.Unix(), 10)
	flows := make([]UserFlow, 0, len(userIDs))
	for _, batch := range splitBatches(userIDs) {
		body := &larksdk.QueryUserFlowReqBody{
			UserIds:       batch,
			CheckTimeFrom: &from,
			CheckTimeTo:   &to,
		}
		req := larksdk.NewQueryUserFlowReqBuilder().
			EmployeeType(userEmployeeType).
			Body(body).
			Build()
		var resp *larksdk.QueryUserFlowResp
		if err := callWithBackoff(ctx, c.sleepOrDefault(), "attendance.user_flows.query",
			func() (*larkcore.ApiResp, larkcore.CodeError, error) {
				r, callErr := c.userFlows.Query(ctx, req)
				resp = r
				if r == nil {
					return nil, larkcore.CodeError{}, callErr
				}
				return r.ApiResp, r.CodeError, callErr
			}); err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil {
			continue
		}
		for _, flow := range resp.Data.UserFlowResults {
			if flow == nil {
				continue
			}
			flows = append(flows, convertUserFlow(flow))
		}
	}
	return flows, nil
}

// convertUserTask 把 SDK 的打卡结果投影为本包的 UserTask。
func convertUserTask(task *larksdk.UserTask) UserTask {
	out := UserTask{
		UserID:       stringValue(task.UserId),
		EmployeeName: stringValue(task.EmployeeName),
		Day:          intValue(task.Day),
		GroupID:      stringValue(task.GroupId),
		ShiftID:      stringValue(task.ShiftId),
		Tasks:        make([]UserTaskResult, 0, len(task.Records)),
	}
	for _, record := range task.Records {
		if record == nil {
			continue
		}
		out.Tasks = append(out.Tasks, UserTaskResult{
			CheckInRecordID:  stringValue(record.CheckInRecordId),
			CheckInRecord:    convertUserFlowPtr(record.CheckInRecord),
			CheckOutRecordID: stringValue(record.CheckOutRecordId),
			CheckOutRecord:   convertUserFlowPtr(record.CheckOutRecord),
			TaskShiftType:    intValue(record.TaskShiftType),
		})
	}
	return out
}

// convertUserFlowPtr 投影打卡结果里内嵌的完整流水；飞书未返回该对象时返回 nil。
func convertUserFlowPtr(flow *larksdk.UserFlow) *UserFlow {
	if flow == nil {
		return nil
	}
	out := convertUserFlow(flow)
	return &out
}

// convertUserFlow 把 SDK 的流水投影为本包的 UserFlow。
func convertUserFlow(flow *larksdk.UserFlow) UserFlow {
	return UserFlow{
		UserID:       stringValue(flow.UserId),
		RecordID:     stringValue(flow.RecordId),
		CheckTime:    stringValue(flow.CheckTime),
		Type:         intValue(flow.Type),
		IsField:      boolValue(flow.IsField),
		IsWifi:       boolValue(flow.IsWifi),
		Ssid:         stringValue(flow.Ssid),
		Bssid:        stringValue(flow.Bssid),
		LocationName: stringValue(flow.LocationName),
		Latitude:     floatValue(flow.Latitude),
		Longitude:    floatValue(flow.Longitude),
		PhotoUrls:    append([]string(nil), flow.PhotoUrls...),
		Comment:      stringValue(flow.Comment),
		CheckResult:  stringValue(flow.CheckResult),
	}
}

// splitBatches 把人员列表按 batchSize 切片；空列表返回空切片。
func splitBatches(userIDs []string) [][]string {
	if len(userIDs) == 0 {
		return nil
	}
	batches := make([][]string, 0, (len(userIDs)+batchSize-1)/batchSize)
	for start := 0; start < len(userIDs); start += batchSize {
		end := start + batchSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		batches = append(batches, userIDs[start:end])
	}
	return batches
}
