package question

import "fmt"

// Assume records the recommended option as the working default for an open,
// non-blocking question and sets its status to assumed. A blocking question,
// one with no recommended option, or one that is not open is refused and left
// unchanged.
func (q *Question) Assume() error {
	switch {
	case q.Status != StatusOpen:
		return fmt.Errorf("%s is %s, not open", q.ID, q.Status)
	case q.Blocking:
		return fmt.Errorf("%s is blocking; answer it instead of assuming", q.ID)
	case q.Recommended == "":
		return fmt.Errorf("%s has no recommended option to assume", q.ID)
	}
	q.Assumed = q.Recommended
	q.Status = StatusAssumed
	return nil
}

// AssumeResult sorts an item's open questions by what starting work did to
// them.
type AssumeResult struct {
	// Assumed now carry assumed = recommended.
	Assumed []Question
	// Blocking are open blocking questions; they get no assumption.
	Blocking []Question
	// Unassumed are open non-blocking questions with no recommended option.
	Unassumed []Question
}

// Changed reports whether any question was assumed, so the caller knows
// whether to save.
func (r AssumeResult) Changed() bool { return len(r.Assumed) > 0 }

// AssumeOpen assumes the recommended option on every open non-blocking
// question in qs, in place, and reports what it did and what it left open.
func AssumeOpen(qs []Question) AssumeResult {
	var r AssumeResult
	for i := range qs {
		q := &qs[i]
		if q.Status != StatusOpen {
			continue
		}
		switch {
		case q.Blocking:
			r.Blocking = append(r.Blocking, *q)
		case q.Recommended == "":
			r.Unassumed = append(r.Unassumed, *q)
		default:
			_ = q.Assume()
			r.Assumed = append(r.Assumed, *q)
		}
	}
	return r
}

// settle sets the status an answer leaves: confirmed when no default was
// assumed or the answer matches it, overturned when it differs.
func (q *Question) settle() {
	if q.Assumed != "" && q.Answer != q.Assumed {
		q.Status = StatusOverturned
		return
	}
	q.Status = StatusConfirmed
}
