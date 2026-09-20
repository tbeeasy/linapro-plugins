package syncer

// 本文件承载人员列的比较契约：把「一个人员单元格」归约为一组飞书用户 id（open_id），
// 并以集合语义判等。人员列不能按文本比对——Moka 侧持有的是工号、Bitable 回读侧给出的是
// 姓名，二者不在同一空间，直接比字符串会永远不等，导致人员列每轮被误判为变更并空转重写。
// 把比较对象收敛成 id 集合，既是修复，也在类型上钉死「人员身份 = open_id 集合」这一决定，
// 避免后续再退回按姓名比对。

// personSet 是人员列的比较契约：一组飞书用户 id（open_id），去重、顺序无关。
//
// 只用 id 表达身份，不带姓名：姓名是展示名而非标识，同名、改名、双租户同人多身份都会让
// 姓名漂移，用它判等会把同一个人误判为变更。写侧真正落库的也正是这组 id，故以 id 比对
// 能与写入结果严格对称。仅在本包内用于人员列差异比对，故不导出。
type personSet []string

// equal 报告两个 id 集合是否等价：去重后成员完全一致即为相等，与顺序无关。
// 空串 id 视为无效成员并忽略，使「无人员」与「仅含空 id」判定一致。
// 双方均为空集合时返回 true，据此让未变更的人员列走冻结分支。
func (s personSet) equal(other personSet) bool {
	left := s.unique()
	right := other.unique()
	if len(left) != len(right) {
		return false
	}
	for id := range left {
		if _, ok := right[id]; !ok {
			return false
		}
	}
	return true
}

// unique 把切片归约为去重后的 id 集合，并丢弃空串成员。
func (s personSet) unique() map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, id := range s {
		if id == "" {
			continue
		}
		out[id] = struct{}{}
	}
	return out
}
