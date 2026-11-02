// send_status.go 实现能力自检报告（门面 Status）。
//
// 自检供部署验收与告警使用，采集口径是「一次看全」：任何一项取不到都记入该条的 ProbeError
// 或报告的 ConfigIssues，不阻断其它项的采集。

package send

import (
	"context"
	"sort"
	"strings"

	"lina-plugin-linapro-employee-core/backend/cap/empcap"

	"lina-plugin-linapro-lark-push/backend/cap/larkpushcap"
	"lina-plugin-linapro-lark-push/backend/internal/larkcfg"
)

// Status 实现自检端点所需的能力报告：按分组列出各推送应用凭据齐备性、identity_app_id
// 有效性、本企业 user_id 覆盖情况。
func (s *serviceImpl) Status(ctx context.Context) larkpushcap.StatusReport {
	report := larkpushcap.StatusReport{PluginReady: true}
	if s.hostConfig == nil {
		report.ConfigIssues = []string{"宿主配置能力不可用"}
		return report
	}
	groups, err := larkcfg.Load(ctx, s.hostConfig)
	if err != nil {
		report.ConfigIssues = []string{err.Error()}
		return report
	}
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)

	// 同一 identity_app_id 只探一次（跨分组共享：多个分组服务同一企业时索引相同）。
	probed := make(map[string]empcap.LarkUserIDIndex)
	probeErr := make(map[string]string)
	for _, group := range names {
		apps := groups[group]
		for _, it := range larkcfg.Validate(group, apps) {
			report.ConfigIssues = append(report.ConfigIssues, it.String())
		}
		for _, app := range apps {
			st := larkpushcap.PushAppStatus{
				Group:           group,
				AppID:           app.AppID,
				Name:            app.Name,
				IdentityAppID:   app.IdentityAppID,
				CredentialReady: strings.TrimSpace(app.AppID) != "" && strings.TrimSpace(app.SecretKey) != "",
			}
			idx, ok := probed[app.IdentityAppID]
			if !ok {
				if _, done := probeErr[app.IdentityAppID]; !done {
					idx, err = s.indexPath(ctx, app.IdentityAppID)
					if err != nil {
						probeErr[app.IdentityAppID] = err.Error()
					} else {
						probed[app.IdentityAppID] = idx
					}
				}
				if msg, failed := probeErr[app.IdentityAppID]; failed {
					st.ProbeError = msg
					report.PushApps = append(report.PushApps, st)
					continue
				}
			}
			st.AddressableEmployees = len(idx.ByEmployeeNo)
			st.BlankUserIDEmployees = len(idx.BlankEmployeeNos)
			st.Collisions = len(idx.CollidedEmployeeNos)
			report.PushApps = append(report.PushApps, st)
		}
	}
	report.ConfigValid = len(report.ConfigIssues) == 0
	// 稳定排序（分组名、App ID），便于不同节点/两次调用的报告可直接 diff。
	sort.SliceStable(report.PushApps, func(i, j int) bool {
		a, b := report.PushApps[i], report.PushApps[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		return a.AppID < b.AppID
	})
	return report
}
