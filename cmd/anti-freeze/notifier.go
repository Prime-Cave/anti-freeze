package main

import (
	"context"
	"sync"

	"github.com/Prime-Cave/anti-freeze/internal/macos"
	"github.com/Prime-Cave/anti-freeze/internal/notify"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// notifier sends Anti-Freeze's notifications through UNUserNotificationCenter
// (the Wails notifications service). The service is started directly rather
// than registered as a bound service, so the frontend can't send
// notifications itself.
type notifier struct {
	ns *notifications.NotificationService

	mu        sync.Mutex
	quitLabel string
}

// newNotifier starts notifications, or returns nil when they are not
// available (for example when running outside the .app bundle).
func newNotifier(onAction func(action, appKey string)) *notifier {
	ns := notifications.New()
	var err error
	application.InvokeSync(func() { err = ns.ServiceStartup(context.Background(), application.ServiceOptions{}) })
	if err != nil {
		return nil
	}
	n := &notifier{ns: ns}
	_ = ns.RegisterNotificationCategory(notifications.NotificationCategory{
		ID:      notify.CategoryDetails,
		Actions: []notifications.NotificationAction{{ID: notify.ActionDetails, Title: "Show details"}},
	})
	ns.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			return
		}
		key, _ := r.Response.UserInfo["appKey"].(string)
		action := notify.ActionDetails
		if r.Response.ActionIdentifier == notify.ActionQuit {
			action = notify.ActionQuit
		}
		onAction(action, key)
	})
	return n
}

// Send delivers a note. The quit category is re-registered when the app
// name changes, so the button reads "Quit Chrome…". It only opens the
// safety check; force quit is never offered here.
func (n *notifier) Send(note notify.Note) error {
	if note.QuitLabel != "" {
		n.mu.Lock()
		changed := note.QuitLabel != n.quitLabel
		n.quitLabel = note.QuitLabel
		n.mu.Unlock()
		if changed {
			if err := n.ns.RegisterNotificationCategory(notifications.NotificationCategory{
				ID: notify.CategoryQuit,
				Actions: []notifications.NotificationAction{
					{ID: notify.ActionQuit, Title: note.QuitLabel},
					{ID: notify.ActionDetails, Title: "Show details"},
				},
			}); err != nil {
				return err
			}
		}
	}
	return n.ns.SendNotificationWithActions(notifications.NotificationOptions{
		ID: note.ID, Title: note.Title, Body: note.Body, CategoryID: note.Category,
		Data:              map[string]interface{}{"appKey": note.AppKey},
		ThreadID:          "anti-freeze",
		InterruptionLevel: "active", // respects Focus; never "time sensitive"
	})
}

// Authorized reports the permission state. determined is false until the
// user has answered the system prompt; Wails only reports granted or not,
// so "not granted" counts as determined.
func (n *notifier) Authorized() (bool, bool) {
	ok, err := n.ns.CheckNotificationAuthorization()
	if err != nil {
		return false, false
	}
	return ok, true
}

// RequestAuthorization shows the system prompt (first time only).
func (n *notifier) RequestAuthorization() bool {
	ok, _ := n.ns.RequestNotificationAuthorization()
	return ok
}

// loginItem adapts internal/macos to app.LoginItem.
type loginItem struct{}

func (loginItem) Status() (on, needsApproval, available bool) {
	switch macos.LoginItemStatus() {
	case macos.LoginOn:
		return true, false, true
	case macos.LoginNeedsApproval:
		return false, true, true
	case macos.LoginOff:
		return false, false, true
	}
	return false, false, false
}

func (loginItem) Set(on bool) error { return macos.SetLoginItem(on) }

func (loginItem) Installed() bool { return macos.InApplications() }
