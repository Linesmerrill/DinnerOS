package liveactivity

import (
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/config"
	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
)

// NewObserver returns the Pusher every process that moves an import job
// hands to mealkit (the worker, and the service that cancels), or nil when
// APNs is not configured — a nil observer is a no-op, and the app still keeps
// its activity current from the status it polls while open.
func NewObserver(apns config.APNs, db *mongo.Database, logger *slog.Logger) (mealkit.ProgressObserver, error) {
	if !apns.Enabled() {
		logger.Info("APNs is not configured; meal-kit Live Activities update only while the app is open")
		return nil, nil
	}
	client, err := push.NewAPNsClient(push.APNsOptions{
		KeyID: apns.KeyID, TeamID: apns.TeamID, Topic: apns.Topic, PrivateKey: apns.PrivateKey,
	})
	if err != nil {
		return nil, err
	}
	return NewPusher(PusherOptions{Store: NewMongoStore(db), Sender: client, Logger: logger}), nil
}
