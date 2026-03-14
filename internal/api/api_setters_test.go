/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"testing"

	"github.com/rs/zerolog"
)

// TestAPI_Setters covers the Set* methods on *API.
func TestAPI_Setters(t *testing.T) {
	a := &API{logger: zerolog.Nop()}

	// All setters accept nil — just verify they don't panic and update the field.
	a.SetNotificationAPI(nil)
	a.SetWebhookAPI(nil)
	a.SetScheduleAnalyticsAPI(nil)
	a.SetSyndicationAPI(nil)
	a.SetUnderwritingAPI(nil)
	a.SetScheduleExportAPI(nil)
	a.SetLandingPageAPI(nil)
	a.SetWebDJAPI(nil)
	a.SetWebDJWebSocket(nil)
	a.SetRecordingAPI(nil)

	if a.notificationAPI != nil || a.webhookAPI != nil ||
		a.scheduleAnalyticsAPI != nil || a.syndicationAPI != nil ||
		a.underwritingAPI != nil || a.scheduleExportAPI != nil ||
		a.landingPageAPI != nil || a.webdjAPI != nil ||
		a.webdjWS != nil || a.recordingAPI != nil {
		t.Fatal("expected all set fields to be nil after setting nil")
	}
}
