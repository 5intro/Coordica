package codequality

// CandidateError is a deterministic defect in the candidate inventory.
// Operational and inventory-bound errors never become a candidate repair.
type CandidateError struct{ Cause error }

func (e *CandidateError) Error() string { return "candidate quality inventory: " + e.Cause.Error() }
func (e *CandidateError) Unwrap() error { return e.Cause }

// SourceError is an immutable per-file encoding or syntax defect. A frozen
// baseline can contain it; every candidate must fix or delete that source.
type SourceError struct{ Cause error }

func (e *SourceError) Error() string { return e.Cause.Error() }
func (e *SourceError) Unwrap() error { return e.Cause }
