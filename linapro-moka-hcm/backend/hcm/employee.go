package hcm

import (
	"context"
	"encoding/json"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
)

const batchDataPath = "/api-platform/hcm/oapi/v2/batch/data"

// APICodeKeyBatchData 是 batch/data（员工列表）接口在 APICodes 映射中的键。
// 使用方必须将 Moka 为该接口下发的 apiCode 填入
// HCMCredential.APICodes[APICodeKeyBatchData]。
const APICodeKeyBatchData = "batchData"

// HCMEmployee 保存员工目录同步所需的员工字段。
// 其余 Moka 字段（薪资、附件、银行卡等）均不解码。
type HCMEmployee struct {
	EmployeeNo string // 工号 (employee_no)，员工目录的稳定外部主键
	Realname   string // 姓名 (realname)，与飞书通讯录的 JOIN 键

	Department   string // 部门名称 (department)，如 "研发部"
	DepartmentID string // 部门 ID (department_id)，People 系统唯一标识

	// EmployeeType 员工类型文本 (employee_type)。常见枚举：正式、实习、外包、劳务派遣。
	EmployeeType string
	// EmployeeTypeID 员工类型 ID (employee_type_id)，People 系统唯一标识。常见枚举：1=正式。
	EmployeeTypeID int

	// EmployeeStatus 在职状态文本 (employee_status)。常见枚举：在职、离职。
	EmployeeStatus string
	// EmployeeStatusID 在职状态 ID (employee_status_id)，People 系统唯一标识。常见枚举：1=在职。
	EmployeeStatusID int
	// Status 归一化在职标志：1=在职，0=离职/其他。
	// 优先取 EmployeeStatusID（1=在职），缺省时取 EmployeeStatus 文本（"在职"→1）。
	Status int

	OnBoardingDate   string // 入职日期 (on_boarding_date)，格式 YYYY-MM-DD
	EndProbationDate string // 转正日期 (end_probation_date)，格式 YYYY-MM-DD
	LeaveDate        string // 离职日期 (leave_date)，格式 YYYY-MM-DD；在职员工为空字符串
}

// employeeRecord 是 batch/data 列表中单个员工的原始 JSON 结构。
// 只解码 HCMEmployee 所需字段，其余字段被丢弃。
type employeeRecord struct {
	EmployeeNo       string `json:"employee_no"`
	Realname         string `json:"realname"`
	Department       string `json:"department"`
	DepartmentID     string `json:"department_id"`
	EmployeeType     string `json:"employee_type"`
	EmployeeTypeID   int    `json:"employee_type_id"`
	EmployeeStatusID int    `json:"employee_status_id"` // 1=在职；0 表示字段缺失
	EmployeeStatus   string `json:"employee_status"`    // 字段缺失时的 "在职" 文本兜底
	OnBoardingDate   string `json:"on_boarding_date"`
	EndProbationDate string `json:"end_probation_date"`
	LeaveDate        string `json:"leave_date"`
}

// toHCMEmployee 将原始记录映射为公开类型。
// Status：优先取 employee_status_id（1=在职）；缺失（0）时使用文本字段。
func (r employeeRecord) toHCMEmployee() HCMEmployee {
	status := 0
	if r.EmployeeStatusID > 0 {
		if r.EmployeeStatusID == 1 {
			status = 1
		}
	} else if r.EmployeeStatus == "在职" {
		status = 1
	}
	return HCMEmployee{
		EmployeeNo:       r.EmployeeNo,
		Realname:         r.Realname,
		Department:       r.Department,
		DepartmentID:     r.DepartmentID,
		EmployeeType:     r.EmployeeType,
		EmployeeTypeID:   r.EmployeeTypeID,
		EmployeeStatus:   r.EmployeeStatus,
		EmployeeStatusID: r.EmployeeStatusID,
		Status:           status,
		OnBoardingDate:   r.OnBoardingDate,
		EndProbationDate: r.EndProbationDate,
		LeaveDate:        r.LeaveDate,
	}
}

type batchDataPage struct {
	Total int              `json:"total"`
	Size  int              `json:"size"`
	List  []employeeRecord `json:"list"`
}

type batchDataEnvelope struct {
	Code int            `json:"code"`
	Msg  string         `json:"msg"`
	Data *batchDataPage `json:"data"`
}

// ListEmployees 从 Moka batch/data 拉取一页员工。
// pageSize 必须 ≤ 200（Moka 限制）。total 是 Moka 上报的总数，
// 调用方可据此判断是否还有更多页。
func (c *Client) ListEmployees(ctx context.Context, pageNum, pageSize int) (list []HCMEmployee, total int, err error) {
	body, err := json.Marshal(map[string]any{
		"pageSize": pageSize,
		"pageNum":  pageNum,
	})
	if err != nil {
		return nil, 0, gerror.Wrap(err, "hcm: marshal ListEmployees body")
	}

	// batch/data 要求以 userName（操作人邮箱）作为签名查询参数，取自凭证 OperatorEmail。
	// 为空时不追加该参数（由 ApplyAuth 跳过空值）。
	extra := map[string]string{"userName": c.cred.OperatorEmail}

	// 打印请求参数
	glog.Debugf(ctx, "moka-hcm: ListEmployees 请求参数 %s userName=%s", string(body), c.cred.OperatorEmail)

	raw, err := c.PostJSON(ctx, batchDataPath, c.cred.APICodes[APICodeKeyBatchData], body, extra)
	if err != nil {
		return nil, 0, err
	}

	// 打印原始返回值
	glog.Debugf(ctx, "moka-hcm: ListEmployees 原始请求返回值 %s", string(raw))

	var env batchDataEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, 0, gerror.Wrapf(err, "hcm: decode ListEmployees response: %s", truncate(string(raw), 512))
	}
	if env.Code != 200 {
		return nil, 0, gerror.Newf("hcm: ListEmployees page=%d returned code=%d msg=%q", pageNum, env.Code, env.Msg)
	}
	if env.Data == nil {
		return nil, 0, nil
	}

	out := make([]HCMEmployee, 0, len(env.Data.List))
	for _, r := range env.Data.List {
		out = append(out, r.toHCMEmployee())
	}

	glog.Debugf(ctx, "moka-hcm: ListEmployees 格式化之后请求返回值 %+v", out)

	return out, env.Data.Total, nil
}

// ListAllEmployees 通过按 200 逐页迭代拉取全部员工，直到累计数量达到 total。
// 调用方无需自行管理分页。
func (c *Client) ListAllEmployees(ctx context.Context) ([]HCMEmployee, error) {
	const pageSize = 200
	var all []HCMEmployee
	for pageNum := 1; ; pageNum++ {
		page, total, err := c.ListEmployees(ctx, pageNum, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(all) >= total || len(page) == 0 {
			break
		}
	}
	return all, nil
}
