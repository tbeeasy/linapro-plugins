// eval-client.ts 封装 360 测评管理端全部 admin API 与 TS 类型。
// 路由前缀：/x/linapro-eval-360/api/v1/eval/admin/*（经 pluginApiPath 拼装）。
// 注意：score/weight 以 string 承载 NUMERIC；allowText/required/published/completed 为 int(0/1)。
import { pluginApiPath, requestClient } from '#/api/request';

const pluginID = 'linapro-eval-360';

function evalApi(pathName: string) {
  return pluginApiPath(pluginID, `eval/admin/${pathName}`);
}

// ---- 通用 ----

export interface PingResult {
  plugin: string;
  ready: boolean;
}

export function evalPing() {
  return requestClient.get<PingResult>(evalApi('ping'));
}

// ---- 项目 ----

export type ProjectStatus = 'draft' | 'active' | 'closed';

export interface ProjectListItem {
  id: number;
  name: string;
  description: string;
  status: ProjectStatus;
  startAt: null | number; // Unix 毫秒时间戳
  endAt: null | number; // Unix 毫秒时间戳
  closedAt: null | number; // Unix 毫秒时间戳
}

export interface QuestionnaireCard {
  id: number;
  title: string;
  published: number; // 0/1
  sort: number;
  evaluateeCount: number;
  completed: number;
  expected: number;
  createdAt: null | number; // Unix 毫秒时间戳
}

export interface ProjectDetail extends ProjectListItem {
  questionnaires: QuestionnaireCard[];
  totalCompleted: number;
  totalExpected: number;
}

export interface ProjectSaveInput {
  name?: string;
  description?: string;
  startAt?: null | number; // Unix 毫秒时间戳
  endAt?: null | number; // Unix 毫秒时间戳
}

export async function projectList() {
  const res = await requestClient.get<{ list: ProjectListItem[] }>(
    evalApi('project'),
  );
  return res.list;
}

export function projectDetail(id: number) {
  return requestClient.get<{ detail: ProjectDetail }>(
    evalApi(`project/${id}`),
  );
}

export function projectCreate(data: ProjectSaveInput) {
  return requestClient.post<{ id: number }>(evalApi('project'), data);
}

export function projectUpdate(id: number, data: ProjectSaveInput) {
  return requestClient.put(evalApi(`project/${id}`), data);
}

export function projectDelete(id: number) {
  return requestClient.delete(evalApi(`project/${id}`));
}

export function projectClose(id: number) {
  return requestClient.post(evalApi(`project/${id}/close`));
}

export function projectReopen(id: number) {
  return requestClient.post(evalApi(`project/${id}/reopen`));
}

// 一键发送按问卷判定：评价者全部收到飞书卡片的问卷才发布，推送失败的问卷保持未发布并在 failed
// 中逐份列出（部分或全部失败仍正常返回）。单份发布失败仍以业务错误返回（由请求拦截器提示失败名单），
// 其 failed 恒为空。
export type SendFailReason = 'push_failed' | 'unreachable';

export interface SendFailedItem {
  questionnaireId: number;
  title: string;
  reason: SendFailReason;
  employeeNos: string[];
}

export interface SendResult {
  publishedIds: number[];
  failed?: SendFailedItem[];
}

export function projectSend(id: number) {
  return requestClient.post<SendResult>(evalApi(`project/${id}/send`));
}

// ---- 问卷 ----

export interface QuestionnaireListItem {
  id: number;
  projectId: number;
  title: string;
  description: string;
  published: number;
  sort: number;
}

export interface RoleNode {
  id: number;
  name: string;
  weight: string;
}

export interface OptionNode {
  id: number;
  label: string;
  score: string;
  allowText: number;
  sort: number;
}

export interface MatrixColumnNode {
  id: number;
  label: string;
  score: string;
  sort: number;
  // 评语配置：allowComment 为开关；commentRequired 仅开关开启时生效（提交作答时校验）；
  // commentPlaceholder 为评语输入框占位提示；commentPresets 为空表示无预设（自由输入）；
  // commentAllowCustom 表示有预设时是否还允许自定义输入。
  allowComment: number;
  commentRequired: number;
  commentPlaceholder: string;
  commentPresets: string[];
  commentAllowCustom: number;
}

export interface QuestionNode {
  id: number;
  dimensionId: number;
  type: 'single_choice' | 'text_short' | 'text_long' | 'matrix';
  title: string;
  description: string;
  required: number;
  sort: number;
  options: OptionNode[];
  // 矩阵已无手填行：矩阵行由被评估人清单动态渲染，后端不再返回 matrixRows。
  matrixColumns: MatrixColumnNode[];
  visibleRoleIds: number[];
  // 受限评价者 EmpID 名单（排除语义）：名单内的评价者本人在该问卷中看不到该题，与其评谁无关。
  excludedEvaluatorEmpIds: number[];
}

export interface DimensionNode {
  id: number;
  name: string;
  sort: number;
  questions: QuestionNode[];
}

export interface QuestionnaireDetail {
  id: number;
  projectId: number;
  title: string;
  description: string;
  published: number;
  // 是否已有任何提交作答。前端据 published||hasAnswers 判断编辑是否受限（草稿且无作答可自由编辑）。
  hasAnswers: boolean;
  sort: number;
  dimensions: DimensionNode[];
  roles: RoleNode[];
}

export interface QuestionnaireSaveInput {
  projectId?: number;
  title?: string;
  description?: string;
  sort?: number;
}

export async function questionnaireList(projectId: number) {
  const res = await requestClient.get<{ list: QuestionnaireListItem[] }>(
    evalApi('questionnaire'),
    { params: { projectId } },
  );
  return res.list;
}

// 问卷分页：返回 { items, total }（字段名匹配 vxe-table 代理约定）。
// items 为含完成率统计的问卷卡片，支持按标题模糊搜索。
export interface QuestionnairePageResult {
  items: QuestionnaireCard[];
  total: number;
}

export function questionnairePage(params: {
  keyword?: string;
  pageNum: number;
  pageSize: number;
  projectId: number;
}) {
  return requestClient.get<QuestionnairePageResult>(
    evalApi('questionnaire/page'),
    { params },
  );
}

export function questionnaireDetail(id: number) {
  return requestClient.get<{ detail: QuestionnaireDetail }>(
    evalApi(`questionnaire/${id}`),
  );
}

export function questionnaireCreate(data: QuestionnaireSaveInput) {
  return requestClient.post<{ id: number }>(evalApi('questionnaire'), data);
}

export function questionnaireUpdate(id: number, data: QuestionnaireSaveInput) {
  return requestClient.put(evalApi(`questionnaire/${id}`), data);
}

export function questionnaireDelete(id: number) {
  return requestClient.delete(evalApi(`questionnaire/${id}`));
}

// 发布单份问卷并向其评价者推送飞书卡片；只返回本次发布的问卷 id（已发布跳过时为空、无 failed），
// 推送失败以业务错误返回。
export function questionnairePublish(id: number) {
  return requestClient.post<SendResult>(evalApi(`questionnaire/${id}/publish`));
}

export function questionnaireUnpublish(id: number) {
  return requestClient.post(evalApi(`questionnaire/${id}/unpublish`));
}

// 复制问卷：title 为新问卷名称（同项目内不可与其他问卷重名，由前端弹窗指定）。
export function questionnaireCopy(id: number, title: string) {
  return requestClient.post<{ id: number }>(
    evalApi(`questionnaire/${id}/copy`),
    { title },
  );
}

export function questionnaireClearAnswers(id: number) {
  return requestClient.post(evalApi(`questionnaire/${id}/clear-answers`));
}

// ---- 维度 ----

export interface DimensionSaveInput {
  questionnaireId?: number;
  name?: string;
  sort?: number;
}

export function dimensionCreate(data: DimensionSaveInput) {
  return requestClient.post<{ id: number }>(evalApi('dimension'), data);
}

export function dimensionUpdate(id: number, data: DimensionSaveInput) {
  return requestClient.put(evalApi(`dimension/${id}`), data);
}

export function dimensionDelete(id: number) {
  return requestClient.delete(evalApi(`dimension/${id}`));
}

// ---- 题目 ----

export interface QuestionOptionDTO {
  id: number; // 0=新增
  label: string;
  score: string;
  allowText: number;
  sort: number;
}

export interface MatrixColumnDTO {
  id: number; // 0=新增
  label: string;
  score: string;
  sort: number;
  allowComment: number;
  commentRequired: number;
  commentPlaceholder: string;
  commentPresets: string[];
  commentAllowCustom: number;
}

export interface QuestionSaveInput {
  questionnaireId: number;
  dimensionId: number;
  type: 'single_choice' | 'text_short' | 'text_long' | 'matrix';
  title: string;
  description: string;
  required: number;
  sort: number;
  options: QuestionOptionDTO[];
  // 矩阵已无手填行：题目定义只需列（评价项），行由被评估人清单动态渲染，无需上送。
  matrixColumns: MatrixColumnDTO[];
  visibleRoleIds: number[];
  // 受限评价者 EmpID 名单，保存时整体替换；空=不限制。
  excludedEvaluatorEmpIds: number[];
}

export function questionCreate(data: QuestionSaveInput) {
  return requestClient.post<{ id: number }>(evalApi('question'), data);
}

export function questionUpdate(id: number, data: QuestionSaveInput) {
  return requestClient.put<{ id: number }>(evalApi(`question/${id}`), data);
}

export function questionDelete(id: number) {
  return requestClient.delete(evalApi(`question/${id}`));
}

// 题目重排：questionIds 为该维度全部题目按目标顺序排列的完整 id 列表（不重不漏）。
// 用于「在某题之后插入新题」后把整体 sort 顺延为 0 起连续值。
export function questionReorder(
  questionnaireId: number,
  dimensionId: number,
  questionIds: number[],
) {
  return requestClient.put(evalApi('question/reorder'), {
    questionnaireId,
    dimensionId,
    questionIds,
  });
}

// ---- 角色与评价关系 ----

export interface RoleItem {
  id: number;
  name: string;
  weight: string;
}

export async function roleList(questionnaireId: number) {
  const res = await requestClient.get<{ list: RoleItem[] }>(
    evalApi('relation/roles'),
    { params: { questionnaireId } },
  );
  return res.list;
}

export function roleUpdateWeight(id: number, weight: string) {
  return requestClient.put(evalApi(`relation/role/${id}/weight`), { weight });
}

export function roleRename(id: number, name: string) {
  return requestClient.put(evalApi(`relation/role/${id}/rename`), { name });
}

export function roleMerge(fromId: number, toId: number) {
  return requestClient.post(evalApi('relation/role/merge'), {
    fromId,
    toId,
  });
}

// 删除角色。cascade=true 时连带删除该角色下的全部评价关系（角色为问卷级，
// 会同时清掉各被评估人在该角色下的关系）；角色下已有作答时后端拒绝，需先清空作答。
export function roleDelete(id: number, cascade = false) {
  return requestClient.delete(evalApi(`relation/role/${id}`), {
    params: { cascade },
  });
}

export function relationImportTemplate() {
  return requestClient.download<Blob>(evalApi('relation/import-template'));
}

export interface ImportFailItem {
  row: number;
  reason: string;
}

export interface ImportResult {
  success: number;
  fail: number;
  failList: ImportFailItem[];
}

export function relationImport(questionnaireId: number, file: File) {
  const form = new FormData();
  form.append('file', file);
  return requestClient.post<ImportResult>(
    evalApi('relation/import'),
    form,
    { params: { questionnaireId }, headers: { 'Content-Type': 'multipart/form-data' } },
  );
}

export function relationExport(questionnaireId: number) {
  return requestClient.download<Blob>(evalApi('relation/export'), {
    params: { questionnaireId },
  });
}

export interface RelationItem {
  id: number;
  evaluateeId: number;
  evaluatorEmpId: number;
  evaluatorName: string;
  evaluatorNo: string;
  roleId: number;
  roleName: string;
  source: string;
  completed: number;
  derivedStatus: 'pending' | 'filled' | 'voided';
}

export interface RoleGroup {
  roleId: number;
  roleName: string;
  items: RelationItem[];
}

export interface EvaluateeRow {
  evaluateeId: number;
  evaluateeEmpId: number;
  evaluateeName: string;
  evaluateeNo: string;
  // 被评估人职务/部门（问卷级录入文本，未填写为空串）。
  evaluateeJobTitle: string;
  evaluateeDepartment: string;
  groups: RoleGroup[];
}

export interface RelationMatrix {
  questionnaireId: number;
  evaluatees: EvaluateeRow[];
  roles: RoleItem[];
}

export async function relationList(questionnaireId: number) {
  const res = await requestClient.get<{ matrix: RelationMatrix }>(
    evalApi('relation'),
    { params: { questionnaireId } },
  );
  return res.matrix;
}

export interface EmployeeOption {
  empId: number;
  employeeNo: string;
  name: string;
  department: string;
}

export async function searchEmployees(keyword: string) {
  const res = await requestClient.get<{ list: EmployeeOption[] }>(
    evalApi('relation/search-employees'),
    { params: { keyword } },
  );
  return res.list;
}

// 一条待新增的评价关系：只录入姓名，不录入工号（后端按在职员工姓名匹配）。
export interface AddRelationItem {
  evaluateeName: string;
  // 可选：被评估人已存在时，非空值覆盖原值、空/缺省保留原值。
  evaluateeJobTitle?: string;
  evaluateeDepartment?: string;
  evaluatorName: string;
  roleName: string;
}

export interface AddRelationInput {
  questionnaireId: number;
  // 单次 1~500 条；任一姓名在职重名或找不到时整批拒绝、不写入任何数据。
  items: AddRelationItem[];
}

// 一次批量新增评价关系，返回的 ids 与 items 顺序一致。
export function relationAdd(data: AddRelationInput) {
  return requestClient.post<{ ids: number[] }>(evalApi('relation'), data);
}

export function relationUpdateRole(id: number, roleId: number) {
  return requestClient.put(evalApi(`relation/${id}/role`), { roleId });
}

export function relationDelete(id: number) {
  return requestClient.delete(evalApi(`relation/${id}`));
}

// 删除被评估人及其全部评价关系；其下任一评价关系已有作答时后端拒绝。
export function evaluateeDelete(id: number) {
  return requestClient.delete(evalApi(`relation/evaluatee/${id}`));
}

// ---- 明细导出（两个独立按钮，各直出单个 Excel） ----

export function exportByAnswer(questionnaireId: number) {
  return requestClient.download<Blob>(evalApi('export/by-answer'), {
    params: { questionnaireId },
  });
}

export function exportByScore(questionnaireId: number) {
  return requestClient.download<Blob>(evalApi('export/by-score'), {
    params: { questionnaireId },
  });
}



