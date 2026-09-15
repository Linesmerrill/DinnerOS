// Package users owns the internal User and the AuthIdentity records that link
// identity-provider subjects (Apple, Google) to a user.
//
// A user is created the first time a verified provider identity signs in.
// Accounts are never merged by email: each (provider, subject) pair belongs to
// exactly one user, and linking another login method is an explicit,
// authenticated action (not yet implemented).
package users

import (
	"errors"
	"time"
)

// Provider identifies where an AuthIdentity was verified.
type Provider string

// Supported providers. ProviderDev exists only for local development and tests;
// its route is never mounted in production.
const (
	ProviderApple  Provider = "apple"
	ProviderGoogle Provider = "google"
	ProviderDev    Provider = "dev"
)

// Errors returned by stores and the service.
var (
	ErrNotFound  = errors.New("users: not found")
	ErrDuplicate = errors.New("users: duplicate")
)

// User is a DinnerOS account. ID is an opaque hex string.
type User struct {
	ID           string
	DisplayName  string
	PrimaryEmail string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AuthIdentity links a provider's stable subject to a User. Email is recorded
// only when the provider verified it.
type AuthIdentity struct {
	ID            string
	UserID        string
	Provider      Provider
	Subject       string
	Email         string
	EmailVerified bool
	CreatedAt     time.Time
	LastUsedAt    time.Time
}

// VerifiedIdentity is what an identity verifier established from a provider
// token. Email must be empty unless the provider verified it. DisplayName is a
// hint used only when a new user is created.
type VerifiedIdentity struct {
	Provider      Provider
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
}
