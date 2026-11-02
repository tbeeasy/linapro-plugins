// employee-client.ts 封装后台「通讯录管理 → 员工列表」的接口调用与数据投影。
import { pluginApiPath, requestClient } from "#/api/request";

const pluginID = "linapro-employee-core";

function employeeApi(pathName: string) {
  return pluginApiPath(pluginID, pathName);
}

/** 列表行。飞书身份只给 app_id 摘要，open_id 只在详情里出现。 */
export interface EmployeeItem {
  empId: number;
  name: string;
  mokaEmployeeNo: string;
  tenantId: number;
  tenantName: string;
  /** 1=在职 0=离职 */
  status: number;
  larkBound: boolean;
  larkAppIds: string[];
  updatedAt: null | number;
}

export interface EmployeeListParams {
  pageNum?: number;
  pageSize?: number;
  keyword?: string;
  tenantId?: number;
  status?: number;
  larkBound?: boolean;
}

/** 一条飞书身份映射。 */
export interface LarkIdentityItem {
  larkAppId: string;
  larkAppName: string;
  openId: string;
  userId: string;
  /** own=本企业身份（可登录）；assoc=关联组织视角（不可登录） */
  idSource: string;
  createdAt: null | number;
}

/** 详情：列表字段 + 关联后台账号 + 部门 + 全部飞书身份。 */
export interface EmployeeDetail {
  empId: number;
  name: string;
  mokaEmployeeNo: string;
  tenantId: number;
  tenantName: string;
  status: number;
  sysUserId: number;
  sysUserName: string;
  sysUserNickname: string;
  createdAt: null | number;
  updatedAt: null | number;
  departmentIds: string[];
  larkIdentities: LarkIdentityItem[];
}

export interface LarkAppOption {
  larkAppId: string;
  name: string;
}

export interface SysUserOption {
  id: number;
  username: string;
  nickname: string;
}

export async function listEmployees(params: EmployeeListParams) {
  const res = await requestClient.get<{ list: EmployeeItem[]; total: number }>(
    employeeApi("contacts/employees"),
    { params },
  );
  return { items: res.list, total: res.total };
}

export async function getEmployee(empId: number) {
  return requestClient.get<EmployeeDetail>(
    employeeApi(`contacts/employees/${empId}`),
  );
}

export async function bindLarkIdentity(
  empId: number,
  larkAppId: string,
  openId: string,
) {
  return requestClient.post(
    employeeApi(`contacts/employees/${empId}/lark-identities`),
    { empId, larkAppId, openId },
  );
}

export async function unbindLarkIdentity(
  empId: number,
  larkAppId: string,
  openId: string,
) {
  return requestClient.delete(
    employeeApi(`contacts/employees/${empId}/lark-identities`),
    { data: { empId, larkAppId, openId } },
  );
}

export async function setEmployeeSysUser(empId: number, sysUserId: number) {
  return requestClient.put(employeeApi(`contacts/employees/${empId}/sys-user`), {
    empId,
    sysUserId,
  });
}

export async function listLarkApps() {
  const res = await requestClient.get<{ list: LarkAppOption[] }>(
    employeeApi("contacts/employees/lark-apps"),
  );
  return res.list;
}

export async function listSysUserOptions(keyword: string) {
  const res = await requestClient.get<{
    list: SysUserOption[];
    total: number;
  }>(employeeApi("contacts/employees/sys-user-options"), {
    params: { keyword, pageSize: 20 },
  });
  return res.list;
}
