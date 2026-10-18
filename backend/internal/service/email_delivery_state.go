package service

// emailNotSentError certifies that message content has not been submitted to
// SMTP. Errors after writing DATA are deliberately unclassified: retrying those
// could duplicate a message which the server has already accepted.
type emailNotSentError struct{ Err error }

func (e emailNotSentError) Error() string    { return e.Err.Error() }
func (e emailNotSentError) Unwrap() error    { return e.Err }
func emailDefinitelyNotSent(err error) error { return emailNotSentError{Err: err} }
