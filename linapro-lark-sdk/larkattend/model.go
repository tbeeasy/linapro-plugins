// model.go 定义飞书假勤接口在本库内的投影结构体。
//
// 只投影考勤场景真正用到的字段：飞书的判定结果（check_in_result / check_out_result
// 及其 supplement）在本库刻意不投影——本地判定用自己的规则算，不应把飞书判定
// 当作可复用的依据。字段单位与飞书文档一致：时间点是秒级时间戳字符串，
// 分钟类字段单位为分钟。指针字段一律取零值安全转换。

package larkattend

// GroupMeta 是考勤组的轻量条目，来自「查询所有考勤组」接口。
type GroupMeta struct {
	// GroupID 是飞书考勤组 ID；组删除重建后会变。
	GroupID string
	// GroupName 是考勤组名称。
	GroupName string
}

// GroupDetail 是考勤组详情中常用的配置字段。
type GroupDetail struct {
	// GroupID 是飞书考勤组 ID。
	GroupID string
	// GroupName 是考勤组名称。
	GroupName string
	// GroupType 是组类型原值：0=固定班制 2=排班制 3=自由班次。
	GroupType int
	// PunchDayShiftIDs 是固定班制的「周一至周日 → 班次 id」映射，顺序即星期顺序。
	PunchDayShiftIDs []string
	// AllowRemedy 表示是否允许补卡。
	AllowRemedy bool
	// RemedyLimitCount 是补卡次数上限。
	RemedyLimitCount int
	// RemedyDateNum 是可补卡的天数范围。
	RemedyDateNum int
	// AllowOutPunch 表示是否允许外勤打卡。
	AllowOutPunch bool
	// OutPunchNeedPostApproval 表示外勤打卡是否需要事后审批。
	OutPunchNeedPostApproval bool
	// NeedPunchMemberCount 是需打卡成员数（只用于展示与对账，不参与判定）。
	NeedPunchMemberCount int
}

// Shift 是班次定义中常用的字段，来自「查询所有班次」接口。
type Shift struct {
	// ShiftID 是飞书班次 ID；班次删除重建后会变。
	ShiftID string
	// ShiftName 是班次名称。
	ShiftName string
	// PunchTimeRules 是打卡时间段规则。一上一下的班次只有一段；多段班次本库原样返回，
	// 由调用方决定是否只取第一段。
	PunchTimeRules []PunchTimeRule
	// IsFlexible 表示是否为弹性班次。
	IsFlexible bool
	// FlexibleMinutes 是弹性分钟数（未配置 flexible_rule 时生效）。
	FlexibleMinutes int
	// RestRules 是休息段规则。
	RestRules []RestRule
	// LateOffLateOnRules 是晚走晚到规则。
	LateOffLateOnRules []LateOffLateOnRule
}

// PunchTimeRule 是一段上下班打卡规则。
type PunchTimeRule struct {
	// OnTime 是上班时间原值，例如 "09:30"。
	OnTime string
	// OffTime 是下班时间原值；跨天以 24 小时累加表达，例如 "27:00" 表示次日 03:00。
	OffTime string
	// LateMinutesAsLate 是晚到多久记为迟到（分钟）。
	LateMinutesAsLate int
	// LateMinutesAsLack 是晚到多久记为缺卡（分钟）。
	LateMinutesAsLack int
	// LateMinutesAsSeriousLate 是晚到多久记为严重迟到（分钟）。
	LateMinutesAsSeriousLate int
	// EarlyMinutesAsEarly 是早退多久记为早退（分钟）。
	EarlyMinutesAsEarly int
	// EarlyMinutesAsLack 是早退多久记为缺卡（分钟）。
	EarlyMinutesAsLack int
	// OnAdvanceMinutes 是最早可提前多久打上班卡（分钟），即上班打卡窗口的提前量。
	OnAdvanceMinutes int
	// OffDelayMinutes 是最晚可延后多久打下班卡（分钟），即下班打卡窗口的延后量。
	OffDelayMinutes int
}

// RestRule 是一段休息区间，时间写法与上下班时间一致（可能超过 24 点）。
type RestRule struct {
	// BeginTime 是休息开始时间原值。
	BeginTime string
	// EndTime 是休息结束时间原值。
	EndTime string
}

// LateOffLateOnRule 是晚走晚到规则的一段：晚走 N 分钟，次日可晚到 M 分钟。
type LateOffLateOnRule struct {
	// LateOffMinutes 是晚走分钟数。
	LateOffMinutes int
	// LateOnMinutes 是次日可晚到的分钟数。
	LateOnMinutes int
}

// UserTaskQueryResult 是一次「查询打卡结果」的汇总：有效人员的打卡结果，加上被飞书剔除的人员。
//
// 请求带 ignore_invalid_users=true，批内个别人员无效或无权限时飞书仍返回其余人的结果，
// 并把这些人列在 InvalidUserIDs / UnauthorizedUserIDs；调用方据此识别「单人永久错误」，
// 不必因为一个人让整批失败。
type UserTaskQueryResult struct {
	// Tasks 是有效人员的打卡结果，按批顺序拼接。
	Tasks []UserTask
	// InvalidUserIDs 是飞书判定为无效的 user_id（例如已不存在的人员）。
	InvalidUserIDs []string
	// UnauthorizedUserIDs 是当前应用没有数据权限的 user_id。
	UnauthorizedUserIDs []string
}

// UserTask 是一个员工某一天的打卡结果，来自「查询打卡结果」接口。
type UserTask struct {
	// UserID 是飞书 user_id（按请求的 employee_type 口径）。
	UserID string
	// EmployeeName 是员工姓名。
	EmployeeName string
	// Day 是考勤日，格式 yyyyMMdd；这是飞书给出的归属日，调用方不得自行按时间切分。
	Day int
	// GroupID 是考勤组 ID；"1" 表示未加入考勤组。
	GroupID string
	// ShiftID 是班次 ID；"9" 表示默认班次，休息日为 "0"。
	ShiftID string
	// Tasks 是该考勤日的打卡段。
	Tasks []UserTaskResult
}

// UserTaskResult 是一段打卡记录（正常班段或加班班段）。
type UserTaskResult struct {
	// CheckInRecordID 是上班打卡记录 ID。
	CheckInRecordID string
	// CheckInRecord 是上班卡的完整流水，来自响应的 check_in_record；
	// 已实测其键与值和「查询打卡流水」中同 record_id 的流水完全一致，故直接复用 UserFlow。
	// 飞书未返回该对象时为 nil（此时可能仍有 CheckInRecordID）。
	CheckInRecord *UserFlow
	// CheckOutRecordID 是下班打卡记录 ID。
	CheckOutRecordID string
	// CheckOutRecord 是下班卡的完整流水，来自响应的 check_out_record，语义同 CheckInRecord。
	CheckOutRecord *UserFlow
	// TaskShiftType 是班段类型：0=正常班段 1=加班班段。
	TaskShiftType int
}

// UserDailyShift 是某员工某天的排班，来自「查询排班」接口。
type UserDailyShift struct {
	// UserID 是飞书 user_id。
	UserID string
	// GroupID 是考勤组 ID。
	GroupID string
	// ShiftID 是班次 ID；"0" 表示休息日。
	ShiftID string
	// Month 是排班月份，格式 yyyyMM。
	Month int
	// DayNo 是排班日（当月第几天）。
	DayNo int
}

// UserFlow 是一条打卡流水，来自「查询打卡流水」接口。
type UserFlow struct {
	// UserID 是飞书 user_id。
	UserID string
	// RecordID 是打卡记录 ID，与打卡事件推送的 record_id 是同一个值。
	RecordID string
	// CheckTime 是打卡时间（秒级时间戳字符串）；补卡流水保留被补的时间。
	CheckTime string
	// Type 是流水类型：0=本人打卡 2=补卡。
	Type int
	// IsField 表示是否外勤卡（外勤卡经纬度实测为 0，只能依赖地址文字）。
	IsField bool
	// IsWifi 表示是否 Wi-Fi 打卡。
	IsWifi bool
	// Ssid 是 Wi-Fi 名称。
	Ssid string
	// Bssid 是 Wi-Fi 热点 MAC。
	Bssid string
	// LocationName 是打卡地址文字。
	LocationName string
	// Latitude 是纬度；外勤卡实测为 0，不得用于距离校验。
	Latitude float64
	// Longitude 是经度；外勤卡实测为 0，不得用于距离校验。
	Longitude float64
	// PhotoUrls 是打卡照片地址列表。
	PhotoUrls []string
	// Comment 是打卡备注。
	Comment string
	// CheckResult 是飞书的打卡结果标识，仅用于排障对照，不参与本地判定。
	CheckResult string
}
