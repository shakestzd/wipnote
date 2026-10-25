package workitem

// SessionCollection provides read operations for sessions.
// Sessions are primarily created by hooks, not by the SDK directly.
type SessionCollection struct {
	*Collection
}

// NewSessionCollection creates a SessionCollection bound to the given Base.
func NewSessionCollection(base *Base) *SessionCollection {
	return &SessionCollection{Collection: newCollection(base, "sessions", "session")}
}
