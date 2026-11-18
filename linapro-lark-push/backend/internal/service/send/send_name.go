// send_name.go 实现「按姓名寻址」的解析：把带 Name 的收件人解析为工号收件人，
// 解析失败的收件人转成失败项返回，不阻断同批其余收件人。
//
// 匹配口径由数据 owner（empcap）给定、判定口径在本组件：
//   - 姓名按原值精确匹配（仅去首尾空白），不做大小写或全半角归一；
//   - 只认在职员工——离职员工不接收业务消息；
//   - 同一自然人在多个租户各有一行时工号相同，按「工号非空用工号、否则用 EmpID」去重视为一人；
//   - 去重后 0 人找不到、≥2 人重名，都只让该条失败：重名时静默取其一就是发错人，
//     业务上已约定不重名，这里保留防御性判定。
//
// 解析发生在跨企业扇出之前：成功后以工号替换原收件人，其后的扇出、user_id 缺失与碰撞判定
// 完全走既有工号路径，不维护第二条投递链路。

package send

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"lina-core/pkg/bizerr"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkpushcode"
	"lina-plugin-linapro-lark-push/backend/internal/service/alert"
)

// resolveNames 把姓名收件人解析为工号收件人，返回解析后的收件人列表与解析失败项。
//
// 姓名只发起一次批量查询（与收件人数无关），解析结果在内存里判定。返回的失败项是零明细
// （PushAppID 为空），由调用方并入投递结果，使 AllSucceeded() 覆盖它们。
//
// 同一请求里既有显式工号、又有解析到同一人的姓名时，该人只保留一次投递：显式工号优先，
// 姓名项被丢弃（它已由显式项覆盖，再发一次就是重复消息）。
//
// 寻址来源报「能力未就绪」时与工号寻址同样处理：告警一次并返回 CodeEmpcapUnbound，
// 不降级为跳过姓名收件人。
func (s *serviceImpl) resolveNames(
	ctx context.Context,
	group string,
	recipients []larkpushcap.Recipient,
) ([]larkpushcap.Recipient, []larkpushcap.Delivery, error) {
	names := nameRecipients(recipients)
	if len(names) == 0 {
		return recipients, nil, nil
	}

	byName, err := s.namePath(ctx, names)
	if err != nil {
		if errors.Is(err, empcap.ErrServiceUnbound) {
			s.notify(ctx, alert.Event{
				Kind:   alert.KindEmpcapUnbound,
				Group:  group,
				Detail: "姓名寻址依赖 linapro-employee-core 的 empcap，该能力未绑定",
			})
			return nil, nil, bizerr.NewCode(larkpushcode.CodeEmpcapUnbound)
		}
		return nil, nil, gerror.Wrap(err, "lark-push: 按姓名解析员工失败")
	}

	// addressed 是本次已显式给出的工号：姓名解析到同一人时不再插入第二条投递。
	addressed := make(map[string]struct{}, len(recipients))
	for _, r := range recipients {
		if r.EmployeeNo != "" {
			addressed[r.EmployeeNo] = struct{}{}
		}
	}

	var (
		out      = make([]larkpushcap.Recipient, 0, len(recipients))
		failures []larkpushcap.Delivery
	)
	for _, r := range recipients {
		if r.Kind() != larkpushcap.RecipientKindName {
			out = append(out, r)
			continue
		}
		employeeNo, failure := pickActiveByEmployeeName(r.Name, byName[r.Name])
		if failure != nil {
			failures = append(failures, *failure)
			continue
		}
		if _, dup := addressed[employeeNo]; dup {
			continue
		}
		addressed[employeeNo] = struct{}{}
		out = append(out, larkpushcap.Recipient{EmployeeNo: employeeNo, Name: r.Name})
	}
	return out, failures, nil
}

// nameRecipients 取出并去重本批收件人里的姓名（保持出现顺序），空姓名忽略。
func nameRecipients(recipients []larkpushcap.Recipient) []string {
	var (
		out  = make([]string, 0, len(recipients))
		seen = make(map[string]struct{}, len(recipients))
	)
	for _, r := range recipients {
		if r.Kind() != larkpushcap.RecipientKindName {
			continue
		}
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// pickActiveByEmployeeName 判定一个姓名对应的员工：恰好一名在职且有工号时返回其工号，
// 否则返回一条失败明细（NameNotFound / NameDuplicate / Failed）。
func pickActiveByEmployeeName(name string, rows []*empcap.Employee) (string, *larkpushcap.Delivery) {
	var (
		seen   = make(map[string]struct{}, len(rows))
		byNo   []*empcap.Employee
		picked *empcap.Employee
	)
	for _, emp := range rows {
		if emp == nil || !emp.Active() {
			continue
		}
		// 同一工号的多行是同一自然人在不同租户的数据，必须算一人；工号为空时退回 EmpID 去重。
		key := "id:" + strconv.FormatInt(emp.EmpID, 10)
		if emp.EmployeeNo != "" {
			key = "no:" + emp.EmployeeNo
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		byNo = append(byNo, emp)
		if picked == nil {
			picked = emp
		}
	}
	switch len(byNo) {
	case 0:
		return "", nameFailure(name, larkpushcap.DeliveryStatusNameNotFound,
			"员工目录里没有在职员工使用该姓名（写完姓名后未同步，或该员工已离职）")
	case 1:
		if picked.EmployeeNo == "" {
			return "", nameFailure(name, larkpushcap.DeliveryStatusFailed,
				"该员工没有工号，无法按工号寻址")
		}
		return picked.EmployeeNo, nil
	default:
		return "", nameFailure(name, larkpushcap.DeliveryStatusNameDuplicate,
			"有多名在职员工使用该姓名，拒绝发送以避免送错人")
	}
}

// nameFailure 构造一条姓名解析失败的明细（尚未投递任何推送应用）。
func nameFailure(name string, status larkpushcap.DeliveryStatus, reason string) *larkpushcap.Delivery {
	return &larkpushcap.Delivery{Name: name, Status: status, Error: reason}
}
