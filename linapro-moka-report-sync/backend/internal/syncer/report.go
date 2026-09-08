package syncer

// ReportHeader 是 Moka 报表中的一列表头。Children 支持多级表头；
// FlattenReport 会遍历到叶子。
type ReportHeader struct {
	DataIndex string         `json:"dataIndex"`
	Title     string         `json:"title"`
	Type      string         `json:"type"`
	Children  []ReportHeader `json:"children,omitempty"`
}

// ReportData 是 Moka getReportData 调用返回的载荷，
// 已从 HCM 或 Recruit 数据源归一化而来。
type ReportData struct {
	Headers []ReportHeader
	Rows    []map[string]any
}
