// employee_lark_identity.go 实现飞书身份映射（lark_identity 表）的解析与批量寻址。
//
// 身份语义：id_source='own' 是本企业原生身份，可作登录凭据与考勤反查；
// id_source='assoc' 是关联组织视角身份，仅供写飞书多维表格人员字段采集，
// 绝不参与登录与反查。所有 Resolve* 一律加 own 过滤，保证引入 assoc 行后
// 登录仍失败关闭（fail-closed）。
package employee

import (
	"context"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"
	"lina-plugin-linapro-employee-core/backend/internal/dao"
	"lina-plugin-linapro-employee-core/backend/internal/model/do"
	"lina-plugin-linapro-employee-core/backend/internal/model/entity"
)

// ResolveEmpIDByLarkIdentity 通过 lark_identity 表返回由
// (larkAppID, openID) 映射的 EmpID。未注册时返回 ErrEmployeeNotFound，
// 使登录按失败关闭处理（绝不自动开通）。
func (s *Service) ResolveEmpIDByLarkIdentity(ctx context.Context, larkAppID, openID string) (int64, error) {
	var row *entity.LarkIdentity
	// 仅匹配 id_source='own'：assoc 行是关联组织视角身份、仅供写人员字段采集，
	// 绝不作登录凭据。加此过滤后，即便同一 (lark_app_id, open_id) 以 assoc 登记，
	// 登录仍查不到、返回 ErrEmployeeNotFound（fail-closed）。
	if err := dao.LarkIdentity.Ctx(ctx).
		Where(do.LarkIdentity{LarkAppId: larkAppID, LarkOpenId: openID, IdSource: empcap.IDSourceOwn}).
		Scan(&row); err != nil {
		return 0, err
	}
	if row == nil {
		return 0, empcap.ErrEmployeeNotFound
	}
	return row.EmpId, nil
}

// ResolveEmpIDByLarkUserID 通过 lark_identity 表按 (larkAppID, userID) 双键
// 返回映射的 EmpID，供考勤等只带 user_id 的数据源反查员工。同一企业配多个
// app 时同人会有多行且 user_id 相同，均指向同一 emp_id，取任意一行即可。
// 未命中或该行 user_id 为空时返回 ErrEmployeeNotFound。
func (s *Service) ResolveEmpIDByLarkUserID(ctx context.Context, larkAppID, userID string) (int64, error) {
	if userID == "" {
		return 0, empcap.ErrEmployeeNotFound
	}
	var row *entity.LarkIdentity
	// 同样加 id_source='own' 过滤作双保险：assoc 行 lark_user_id 恒空、天然不命中，
	// 但显式过滤可防未来接口返 user_id 时 assoc 行泄漏进考勤反查。
	if err := dao.LarkIdentity.Ctx(ctx).
		Where(do.LarkIdentity{LarkAppId: larkAppID, LarkUserId: userID, IdSource: empcap.IDSourceOwn}).
		Scan(&row); err != nil {
		return 0, err
	}
	if row == nil {
		return 0, empcap.ErrEmployeeNotFound
	}
	return row.EmpId, nil
}

// UpsertLarkIdentity 写入 (larkAppID, openID) → empID 映射，并一并落库
// userID（企业级 user_id，供考勤反查）。键已存在时更新目标 empID、userID 与
// idSource，否则插入新行。
//
// 以主键 (lark_app_id, lark_open_id) 为冲突键做单条原子 upsert（GoFrame
// Save + OnConflict，由驱动生成对应方言），一次数据库往返；并发执行的两次同步
// 不会因「先查后插」的竞态撞主键。冲突更新时 created_at 保持首次写入值。
func (s *Service) UpsertLarkIdentity(ctx context.Context, larkAppID, openID, userID, idSource string, empID int64) error {
	if idSource == "" {
		idSource = empcap.IDSourceOwn
	}
	cols := dao.LarkIdentity.Columns()
	_, err := dao.LarkIdentity.Ctx(ctx).
		Data(do.LarkIdentity{
			LarkAppId:  larkAppID,
			LarkOpenId: openID,
			LarkUserId: userID,
			IdSource:   idSource,
			EmpId:      empID,
		}).
		OnConflict(cols.LarkAppId, cols.LarkOpenId).
		Save()
	return err
}

// MapLarkOpenIDsByEmployeeNo 以 Moka 员工工号（employee.moka_employee_no）为查找键：
// 一次查询 JOIN employee 与 lark_identity，返回「工号 → 该 app 作用域下全部 open_id」映射，
// 按 larkAppID 过滤 lark_identity，跨租户、不限在职、不按 id_source 过滤（own + assoc 都取，
// 双租户同人多个 open_id 全返回）。工号为精确锚点，无需姓名归一——空工号的员工行直接跳过。
//
// 供 recruit-pipeline / report-sync 写人员字段（消费方经门面 LarkOpenIDResolverByEmployeeNos
// 复用该批量结果，无 N+1）。注意：open_id 按应用作用域签发，必须按写表格的 larkAppID 过滤，
// 写侧以 user_id_type=open_id 识别人员字段；不过滤会混入其它应用作用域的 id，写入即错。
func (s *Service) MapLarkOpenIDsByEmployeeNo(ctx context.Context, larkAppID string) (map[string][]string, error) {
	// 只取工号与 open_id 两列；按 emp_id 关联，按 lark_app_id 过滤（限定写表格应用作用域）。
	empCols := dao.Employee.Columns()
	idCols := dao.LarkIdentity.Columns()
	var rows []struct {
		EmployeeNo string `orm:"moka_employee_no"`
		OpenID     string `orm:"lark_open_id"`
	}
	err := dao.Employee.Ctx(ctx).
		As("e").
		InnerJoin(dao.LarkIdentity.Table()+" li", "li."+idCols.EmpId+" = e."+empCols.Id).
		Where("li."+idCols.LarkAppId, larkAppID).
		Fields("e."+empCols.MokaEmployeeNo+" AS moka_employee_no", "li."+idCols.LarkOpenId+" AS lark_open_id").
		Scan(&rows)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(rows))
	seen := make(map[string]map[string]struct{}, len(rows)) // 工号 → open_id 去重集
	for _, r := range rows {
		if r.EmployeeNo == "" || r.OpenID == "" {
			continue
		}
		if seen[r.EmployeeNo] == nil {
			seen[r.EmployeeNo] = make(map[string]struct{})
		}
		if _, dup := seen[r.EmployeeNo][r.OpenID]; dup {
			continue
		}
		seen[r.EmployeeNo][r.OpenID] = struct{}{}
		out[r.EmployeeNo] = append(out[r.EmployeeNo], r.OpenID)
	}
	return out, nil
}

// MapOwnLarkOpenIDsByEmployeeNo 是面向出站消息推送的只读寻址：以 Moka 工号为键，
// 返回「工号 → 该 app 作用域下 id_source='own' 的 open_id」映射。它与
// MapLarkOpenIDsByEmployeeNo 的区别在于只取 own 身份——assoc 为他企业视角身份、
// 无法经本应用投递消息，必须排除。按 larkAppID 作用域过滤、跨所有租户、不限在职。
// 同一 (lark_app_id, emp) 下 own 行至多一条，故返回单值 map；若异常出现多条 own 行
// 取首条非空。工号为空或无 own 映射的员工不入 map。供 linapro-eval-360 遍历各 app
// 取 own 映射、用该 app 凭据当场发送飞书卡片。无任何映射时返回空 map。
func (s *Service) MapOwnLarkOpenIDsByEmployeeNo(ctx context.Context, larkAppID string) (map[string]string, error) {
	empCols := dao.Employee.Columns()
	idCols := dao.LarkIdentity.Columns()
	var rows []struct {
		EmployeeNo string `orm:"moka_employee_no"`
		OpenID     string `orm:"lark_open_id"`
	}
	err := dao.Employee.Ctx(ctx).
		As("e").
		InnerJoin(dao.LarkIdentity.Table()+" li", "li."+idCols.EmpId+" = e."+empCols.Id).
		Where("li."+idCols.LarkAppId, larkAppID).
		Where("li."+idCols.IdSource, empcap.IDSourceOwn).
		Fields("e."+empCols.MokaEmployeeNo+" AS moka_employee_no", "li."+idCols.LarkOpenId+" AS lark_open_id").
		Scan(&rows)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.EmployeeNo == "" || r.OpenID == "" {
			continue
		}
		if _, exists := out[r.EmployeeNo]; !exists {
			// 同一工号仅取首条 own open_id（own 在 (app, emp) 下应唯一）。
			out[r.EmployeeNo] = r.OpenID
		}
	}
	return out, nil
}

// MapOwnLarkUserIDsByEmployeeNo 是 MapOwnLarkOpenIDsByEmployeeNo 的 user_id 版本：以 Moka
// 工号为键，返回该 app 作用域下 own 身份的寻址索引（语义见 empcap.LarkUserIDIndex）。
// 按 larkAppID 作用域过滤、跨所有租户、不限在职。
// 供 linapro-lark-push 按推送应用的 identity_app_id 查表投递。
func (s *Service) MapOwnLarkUserIDsByEmployeeNo(ctx context.Context, larkAppID string) (empcap.LarkUserIDIndex, error) {
	empCols := dao.Employee.Columns()
	idCols := dao.LarkIdentity.Columns()
	var rows []larkUserIDRow
	err := dao.Employee.Ctx(ctx).
		As("e").
		InnerJoin(dao.LarkIdentity.Table()+" li", "li."+idCols.EmpId+" = e."+empCols.Id).
		Where("li."+idCols.LarkAppId, larkAppID).
		Where("li."+idCols.IdSource, empcap.IDSourceOwn).
		Fields("e."+empCols.MokaEmployeeNo+" AS moka_employee_no", "li."+idCols.LarkUserId+" AS lark_user_id").
		Scan(&rows)
	if err != nil {
		return empcap.LarkUserIDIndex{}, err
	}
	return buildLarkUserIDIndex(rows), nil
}

// larkUserIDRow 是「工号 + user_id」查询的中间行。
type larkUserIDRow struct {
	EmployeeNo string `orm:"moka_employee_no"`
	UserID     string `orm:"lark_user_id"`
}

// buildLarkUserIDIndex 把查询行按工号归入三个集合：抽成纯函数以便单测覆盖。
//
// 规则：
//   - 工号为空 → 忽略（无法按工号寻址）；
//   - user_id 为空 → 记入 BlankEmployeeNos；
//   - 同一工号出现第二个不同的 user_id → 移出 ByEmployeeNo、记入 CollidedEmployeeNos，之后同工号的行
//     不再让它回到 ByEmployeeNo；同一 user_id 的重复行（如多部门 JOIN）不算碰撞。
//
// 最后从 BlankEmployeeNos 去掉已可投递或已碰撞的工号，保证三个集合互不相交（同一工号既有空行又有
// 有效行时，以有效行为准）。
func buildLarkUserIDIndex(rows []larkUserIDRow) empcap.LarkUserIDIndex {
	idx := empcap.LarkUserIDIndex{
		ByEmployeeNo:        make(map[string]string, len(rows)),
		BlankEmployeeNos:    make(map[string]struct{}),
		CollidedEmployeeNos: make(map[string]struct{}),
	}
	for _, r := range rows {
		switch {
		case r.EmployeeNo == "":
			continue
		case r.UserID == "":
			idx.BlankEmployeeNos[r.EmployeeNo] = struct{}{}
			continue
		}
		if _, collided := idx.CollidedEmployeeNos[r.EmployeeNo]; collided {
			continue
		}
		prev, exists := idx.ByEmployeeNo[r.EmployeeNo]
		if !exists {
			idx.ByEmployeeNo[r.EmployeeNo] = r.UserID
			continue
		}
		if prev == r.UserID {
			continue
		}
		idx.CollidedEmployeeNos[r.EmployeeNo] = struct{}{}
		delete(idx.ByEmployeeNo, r.EmployeeNo)
	}
	for no := range idx.BlankEmployeeNos {
		_, addressable := idx.ByEmployeeNo[no]
		_, collided := idx.CollidedEmployeeNos[no]
		if addressable || collided {
			delete(idx.BlankEmployeeNos, no)
		}
	}
	return idx
}
