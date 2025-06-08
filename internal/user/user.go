// Package user defines the user model used throughout the application,
// particularly for authentication and user-specific URL storage.
package user

// User represents a system user.
// It contains the unique identifier used to associate shortened URLs and sessions.
type User struct {
	// ID is the unique identifier of the user, meaning a UUID.
	ID string
}
