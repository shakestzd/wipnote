package daemon

// Write frames are no longer served (feat-174dc958). The daemon used to apply
// write-op envelopes to its own in-memory projection, which nothing read, and
// no current caller sends one. A frame that is not a read request — from an
// older wipnote binary — gets this one-line error ack, the same shape older
// daemons sent for a version-skewed write, so that binary falls back as it
// always did.

// AckStatus is the outcome field of a write ack.
type AckStatus string

// AckError is the only status this daemon sends for a write frame.
const AckError AckStatus = "error"

// Ack is the reply to a write frame.
type Ack struct {
	Status AckStatus `json:"status"`
	Seq    int64     `json:"seq"`
	Error  string    `json:"error,omitempty"`
}

// writesNotServed is the error an Ack for a write frame carries.
const writesNotServed = "writes are not served: this daemon answers reads only"
