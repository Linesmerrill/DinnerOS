package main

import (
	"log/slog"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/siwa"
)

// siwaHTTPTimeout bounds one call to Apple's token or revoke endpoint.
const siwaHTTPTimeout = 10 * time.Second

// newAppleRevocation wires Sign in with Apple token revocation (decision
// 750). Without SIWA_* it returns a Service that does nothing; the stored
// tokens collection is still purged with each account.
func newAppleRevocation(cfg config.Config, db *mongo.Database, logger *slog.Logger) (*siwa.Service, error) {
	opts := siwa.Options{Store: siwa.NewMongoStore(db), Logger: logger}
	if cfg.SIWA.Enabled() {
		opts.Client = siwa.NewClient(siwa.Credentials{
			TeamID:     cfg.SIWA.TeamID,
			KeyID:      cfg.SIWA.KeyID,
			ClientID:   cfg.SIWA.ClientID,
			PrivateKey: cfg.SIWA.PrivateKey,
		}, siwa.ClientOptions{HTTPClient: &http.Client{Timeout: siwaHTTPTimeout}})
		opts.SealKey = cfg.AuthTokenSigningKey
	} else {
		logger.Warn("SIWA_KEY_ID, SIWA_TEAM_ID, and SIWA_PRIVATE_KEY are not set; Sign in with Apple is not revoked when an account is deleted")
	}
	return siwa.NewService(opts)
}
