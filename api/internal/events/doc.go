// Package events keeps an append-only history of household behavior: recipes
// viewed, rated, planned, cooked, and skipped, grocery items checked off, and
// imports completed. It is the behavioral signal the recommendation engine
// (Autopilot, docs/autopilot.md) learns from.
//
// Server-side modules record through the Recorder interface, usually with
// RecordOrLog so that a failed write is logged and never fails the action that
// produced it. The iOS app sends what only it can observe (views, cooked,
// skipped, checked items) in batches to POST /households/{householdId}/events.
//
// Every event is household-scoped and carries a small payload whose shape is
// fixed per event type (see Payload). Events are never updated or deleted.
package events
