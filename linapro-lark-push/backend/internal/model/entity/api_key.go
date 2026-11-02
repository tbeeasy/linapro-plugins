// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"time"
)

// ApiKey is the golang structure for table api_key.
type ApiKey struct {
	Id            int64      `json:"id"            orm:"id"               description:"Credential ID"`
	Name          string     `json:"name"          orm:"name"             description:"Caller display name"`
	AppKey        string     `json:"appKey"        orm:"app_key"          description:"Public app key carried in the X-App-Key header"`
	Secret        string     `json:"secret"        orm:"secret"           description:"HMAC-SHA256 signing secret"`
	PushGroup     string     `json:"pushGroup"     orm:"push_group"       description:"Bound push group name in push_groups config"`
	Status        int        `json:"status"        orm:"status"           description:"Status from dict sys_normal_disable: 1=normal 0=disabled"`
	Remark        string     `json:"remark"        orm:"remark"           description:"Remark"`
	CreatedAt     *time.Time `json:"createdAt"     orm:"created_at"       description:"Creation time"`
	UpdatedAt     *time.Time `json:"updatedAt"     orm:"updated_at"       description:"Update time"`
	DeletedAt     *time.Time `json:"deletedAt"     orm:"deleted_at"       description:"Deletion time"`
	ChatId        string     `json:"chatId"        orm:"chat_id"          description:"Feishu group chat_id pushed with every request of this credential; empty means no group push"`
	ChatPushAppId string     `json:"chatPushAppId" orm:"chat_push_app_id" description:"Push app ID in the bound group used for the chat; empty means the group default (single push app)"`
}
