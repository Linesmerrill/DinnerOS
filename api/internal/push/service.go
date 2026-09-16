package push

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Service registers and removes a user's device tokens.
type Service struct {
	store  Store
	logger *slog.Logger
	now    func() time.Time
}

// NewService returns a Service. A nil logger discards.
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{store: store, logger: logger, now: time.Now}
}

// normalizeToken lowercases a hex token and checks its shape.
func normalizeToken(token string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(token))
	if !tokenPattern.MatchString(t) || len(t)%2 != 0 {
		return "", invalid("token must be the APNs device token as hex")
	}
	return t, nil
}

// Register stores the caller's device token for environment. Registering the
// same token again (every launch does) only updates it; a token last
// registered by another user moves to this one.
func (s *Service) Register(ctx context.Context, userID, token string, env Environment, platform string) (DeviceToken, error) {
	t, err := normalizeToken(token)
	if err != nil {
		return DeviceToken{}, err
	}
	if !env.Valid() {
		return DeviceToken{}, invalid("environment must be %q or %q", EnvironmentSandbox, EnvironmentProduction)
	}
	if platform == "" {
		platform = PlatformIOS
	}
	if platform != PlatformIOS {
		return DeviceToken{}, invalid("platform must be %q", PlatformIOS)
	}
	now := s.now().UTC().Truncate(time.Millisecond)
	saved, err := s.store.Upsert(ctx, DeviceToken{
		Token: t, UserID: userID, Environment: env, Platform: platform, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return DeviceToken{}, fmt.Errorf("register device token: %w", err)
	}
	return saved, nil
}

// Unregister removes the caller's device token, for sign-out. A token that
// isn't registered to the caller is not an error.
func (s *Service) Unregister(ctx context.Context, userID, token string) error {
	t, err := normalizeToken(token)
	if err != nil {
		return err
	}
	if err := s.store.Delete(ctx, userID, t); err != nil {
		return fmt.Errorf("delete device token: %w", err)
	}
	return nil
}
