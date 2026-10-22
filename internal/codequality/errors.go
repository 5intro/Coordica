package codequality

// CandidateError is a deterministic defect in the candidate inventory.
// Baseline errors remain untyped: repairing a candidate cannot fix its base.
type CandidateError struct{ Cause error }

func (e *CandidateError) Error() string { return "candidate quality inventory: " + e.Cause.Error() }
func (e *CandidateError) Unwrap() error { return e.Cause }
