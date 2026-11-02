// dimension.go 定义 360 测评管理端「维度」API 的请求/响应 DTO。
package v1

import "github.com/gogf/gf/v2/frame/g"

// CreateDimensionReq 在问卷下新增维度。
type CreateDimensionReq struct {
	g.Meta          `path:"/dimension" method:"post" tags:"Eval360 Dimension" summary:"新建维度" dc:"在问卷下新增一个维度。" permission:"eval:360:edit"`
	QuestionnaireId int64  `json:"questionnaireId" v:"required|min:1#缺少问卷 id|问卷 id 非法" dc:"所属问卷 id"`
	Name            string `json:"name" v:"required#请填写维度名称" dc:"维度名称"`
	Sort            int    `json:"sort" dc:"排序"`
}

// CreateDimensionRes 返回新维度 id。
type CreateDimensionRes struct {
	Id int64 `json:"id" dc:"新维度 id"`
}

// UpdateDimensionReq 修改维度名称/排序。
type UpdateDimensionReq struct {
	g.Meta `path:"/dimension/{id}" method:"put" tags:"Eval360 Dimension" summary:"编辑维度" dc:"修改维度名称或排序。" permission:"eval:360:edit"`
	Id     int64   `json:"id" in:"path" v:"required|min:1#缺少维度 id|维度 id 非法" dc:"维度 id"`
	Name   *string `json:"name" dc:"维度名称"`
	Sort   *int    `json:"sort" dc:"排序"`
}

// UpdateDimensionRes 是空响应。
type UpdateDimensionRes struct{}

// DeleteDimensionReq 删除空维度。
type DeleteDimensionReq struct {
	g.Meta `path:"/dimension/{id}" method:"delete" tags:"Eval360 Dimension" summary:"删除维度" dc:"删除维度（含题目时拒绝）。" permission:"eval:360:edit"`
	Id     int64 `json:"id" in:"path" v:"required|min:1#缺少维度 id|维度 id 非法" dc:"维度 id"`
}

// DeleteDimensionRes 是空响应。
type DeleteDimensionRes struct{}
