package notifications

import "context"

// NotificationEvent represents a notification intercepted on the D-Bus bus.
type NotificationEvent struct {
	AppName       string                 `json:"app_name"`
	ReplacesID    uint32                 `json:"replaces_id"`
	AppIcon       string                 `json:"app_icon"`
	AppIconBase64 string                 `json:"app_icon_base64,omitempty"`
	Summary       string                 `json:"summary"`
	Body          string                 `json:"body"`
	Actions       []string               `json:"actions"`
	Hints         map[string]interface{} `json:"hints"`
	ExpireTimeout int32                  `json:"expire_timeout"`
}

// NotificationRequest represents a client request to trigger a host notification via D-Bus.
type NotificationRequest struct {
	AppName       string                 `json:"app_name"`
	ReplacesID    uint32                 `json:"replaces_id"`
	AppIcon       string                 `json:"app_icon"`
	Summary       string                 `json:"summary"`
	Body          string                 `json:"body"`
	Actions       []string               `json:"actions"`
	Hints         map[string]interface{} `json:"hints"`
	ExpireTimeout int32                  `json:"expire_timeout"`
}

// NotificationManager defines the contract for monitoring and triggering D-Bus desktop notifications.
type NotificationManager interface {
	// Start registers a D-Bus session monitor loop and streams intercepted events.
	Start(ctx context.Context) (<-chan NotificationEvent, error)

	// SendNotification issues a standard desktop notification on the host session.
	SendNotification(ctx context.Context, req NotificationRequest) (uint32, error)
}
