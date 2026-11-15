package households

import (
	"context"
	"time"
)

// DeviceProof is an Apple DeviceCheck token the app sent with a request
// (decision 698). Households only carries it: the plus module reads it to
// tell whether this device already belongs to another household.
type DeviceProof struct {
	// Token is DCDevice.generateToken, base64.
	Token string
	// Development is true when a development build made the token, so it is
	// checked against Apple's development DeviceCheck server.
	Development bool
}

// MaxDeviceTokenLength bounds a DeviceCheck token in a request body.
const MaxDeviceTokenLength = 8192

// DeviceCheckRequest is the optional `deviceCheck` object on household
// creation, invitation accept, and referral code entry.
type DeviceCheckRequest struct {
	Token       string `json:"token"`
	Development bool   `json:"development"`
}

// Proof returns the request's proof; ok is false without a token or with
// one too long to be real.
func (r *DeviceCheckRequest) Proof() (DeviceProof, bool) {
	if r == nil || r.Token == "" || len(r.Token) > MaxDeviceTokenLength {
		return DeviceProof{}, false
	}
	return DeviceProof{Token: r.Token, Development: r.Development}, true
}

type deviceProofKey struct{}

// WithDeviceProof returns ctx carrying p.
func WithDeviceProof(ctx context.Context, p DeviceProof) context.Context {
	return context.WithValue(ctx, deviceProofKey{}, p)
}

// DeviceProofFromContext returns the proof the request carried, if any.
func DeviceProofFromContext(ctx context.Context) (DeviceProof, bool) {
	p, ok := ctx.Value(deviceProofKey{}).(DeviceProof)
	return p, ok && p.Token != ""
}

// MemberAdded is a user joining a household, by creating it or by accepting
// an invitation.
type MemberAdded struct {
	HouseholdID string
	UserID      string
	// Created is true when the user made the household.
	Created bool
	At      time.Time
}

// MemberListener hears about every new membership. It runs in the request
// (the request's DeviceProof is in ctx), must return quickly, and cannot fail
// the change. *plus.Service implements it, keeping the membership history
// referrals are checked against.
type MemberListener interface {
	MemberAdded(ctx context.Context, m MemberAdded)
}

// SetMemberListener sets what hears about new memberships, for wiring that
// builds the listener after households. Call it before serving.
func (s *Service) SetMemberListener(l MemberListener) { s.onMember = l }

func (s *Service) memberAdded(ctx context.Context, m Membership, created bool) {
	if s.onMember == nil {
		return
	}
	s.onMember.MemberAdded(ctx, MemberAdded{HouseholdID: m.HouseholdID, UserID: m.UserID, Created: created, At: m.CreatedAt})
}
