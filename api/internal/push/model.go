// Package push delivers household notifications to members' iPhones through
// the Apple Push Notification service (docs/pantry-usage.md#push-delivery).
//
// It has three parts: device tokens each signed-in app registers
// (PUT /api/v1/me/device-tokens), an APNs Sender, and the Sweeper that
// cmd/sendreminders runs on a schedule. The sweep brings derived-on-read
// reminders up to date for every household, then takes pending notifications
// from the notifications outbox and sends each one at most once.
package push

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Errors returned by stores and the service.
var (
	ErrNotFound = errors.New("push: not found")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Environment is the APNs gateway a device token belongs to. Xcode debug
// builds get sandbox tokens; TestFlight and App Store builds get production
// tokens. A token sent to the other gateway is rejected as BadDeviceToken.
type Environment string

// Environments.
const (
	EnvironmentSandbox    Environment = "sandbox"
	EnvironmentProduction Environment = "production"
)

// Valid reports whether e is a known environment.
func (e Environment) Valid() bool {
	return e == EnvironmentSandbox || e == EnvironmentProduction
}

// PlatformIOS is the only platform today.
const PlatformIOS = "ios"

// DeviceToken is one app installation that can receive pushes for a user.
// The token itself is the key: a device signed in by a different user moves
// to that user.
type DeviceToken struct {
	Token       string
	UserID      string
	Environment Environment
	Platform    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// tokenPattern matches an APNs device token as hex. Apple's tokens are 32
// bytes today but Apple says not to rely on the length.
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{16,400}$`)
