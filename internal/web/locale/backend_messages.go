package locale

import (
	"encoding/json"
	"strings"
)

// backendMessageIDs contains the API response messages; browser translations stay in the frontend bundles.
var backendMessageIDs = []string{
	"fail",
	"get",
	"getVersion",
	"pages.clients.hwidDeleted",
	"pages.inbounds.toasts.getNewVlessEncError",
	"pages.inbounds.toasts.getNewX25519CertError",
	"pages.inbounds.toasts.getNewmldsa65Error",
	"pages.inbounds.toasts.inboundClientAddSuccess",
	"pages.inbounds.toasts.inboundClientDeleteSuccess",
	"pages.inbounds.toasts.inboundClientUpdateSuccess",
	"pages.inbounds.toasts.inboundCreateSuccess",
	"pages.inbounds.toasts.inboundDeleteSuccess",
	"pages.inbounds.toasts.inboundUpdateSuccess",
	"pages.inbounds.toasts.logCleanSuccess",
	"pages.inbounds.toasts.obtain",
	"pages.inbounds.toasts.resetAllClientTrafficSuccess",
	"pages.inbounds.toasts.resetAllTrafficSuccess",
	"pages.inbounds.toasts.resetInboundClientTrafficSuccess",
	"pages.inbounds.toasts.resetInboundTrafficSuccess",
	"pages.inbounds.toasts.scanRealityTargetError",
	"pages.inbounds.toasts.trafficGetError",
	"pages.inbounds.toasts.updateSuccess",
	"pages.index.geofileUpdatePopover",
	"pages.index.getConfigError",
	"pages.index.getDatabaseError",
	"pages.index.importDatabaseError",
	"pages.index.importDatabaseSuccess",
	"pages.index.panelUpdateStartedPopover",
	"pages.index.readDatabaseError",
	"pages.index.xraySwitchVersionPopover",
	"pages.login.loginAgain",
	"pages.login.toasts.emptyPassword",
	"pages.login.toasts.emptyUsername",
	"pages.login.toasts.invalidFormData",
	"pages.login.toasts.successLogin",
	"pages.login.toasts.wrongUsernameOrPassword",
	"pages.settings.restartPanelSuccess",
	"pages.settings.subBalancers.toasts.create",
	"pages.settings.subBalancers.toasts.delete",
	"pages.settings.subBalancers.toasts.invalidId",
	"pages.settings.subBalancers.toasts.list",
	"pages.settings.subBalancers.toasts.update",
	"pages.settings.toasts.getOutboundTrafficError",
	"pages.settings.toasts.getSettings",
	"pages.settings.toasts.modifySettings",
	"pages.settings.toasts.modifyUser",
	"pages.settings.toasts.modifyUserError",
	"pages.settings.toasts.originalUserPassIncorrect",
	"pages.settings.toasts.resetOutboundTrafficError",
	"pages.settings.toasts.userPassMustBeNotEmpty",
	"pages.xray.restartError",
	"pages.xray.restartSuccess",
	"pages.xray.stopError",
	"pages.xray.stopSuccess",
	"somethingWentWrong",
}

func unmarshalBackendMessages(data []byte, target any) error {
	var all map[string]any
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	messages := make(map[string]any, len(backendMessageIDs))
	for _, id := range backendMessageIDs {
		var value any = all
		for part := range strings.SplitSeq(id, ".") {
			value = value.(map[string]any)[part]
			if value == nil {
				break
			}
		}
		if value != nil {
			messages[id] = value
		}
	}
	*target.(*any) = messages
	return nil
}
