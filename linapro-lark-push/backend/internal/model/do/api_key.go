// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// ApiKey is the golang structure of table plugin_linapro_lark_push_api_key for DAO operations like Where/Data.
type ApiKey struct {
	g.Meta        `orm:"table:plugin_linapro_lark_push_api_key, do:true"`
	Id            any        // Credential ID
	Name          any        // Caller display name
	AppKey        any        // Public app key carried in the X-App-Key header
	Secret        any        // HMAC-SHA256 signing secret
	PushGroup     any        // Bound push group name in push_groups config
	Status        any        // Status from dict sys_normal_disable: 1=normal 0=disabled
	Remark        any        // Remark
	CreatedAt     *time.Time // Creation time
	UpdatedAt     *time.Time // Update time
	DeletedAt     *time.Time // Deletion time
	ChatId        any        // Feishu group chat_id pushed with every request of this credential; empty means no group push
	ChatPushAppId any        // Push app ID in the bound group used for the chat; empty means the group default (single push app)
}
