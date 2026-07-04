package schema

import "time"

// utcNow keeps every stored timestamp in UTC: SQLite compares time
// columns lexicographically as text, so mixed offsets break every
// <=/>= query (the snooze-wake bug). All writes must go through UTC.
func utcNow() time.Time { return time.Now().UTC() }
