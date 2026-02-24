// Package models centralizes typed platform domain models used by API handlers.
//
// Goal:
// - Reduce repetitive field extraction from bson.M in handlers.
// - Keep mapping and serialization logic in one place.
// - Make gradual refactors safer without changing public API contracts.
package models
