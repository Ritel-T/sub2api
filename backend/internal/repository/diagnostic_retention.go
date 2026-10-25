package repository

import "os"

// Only the explicit opt-in transfers deletion ownership. The external archiver
// must verify two copies before deleting exact, unchanged records. This switch
// deliberately does not depend on its availability or last-success timestamp.
func diagnosticArchiveManaged() bool {
	return os.Getenv("SUB2API_DIAGNOSTIC_ARCHIVE_MANAGED") == "true"
}

// The compatibility field reports the hot retention target in managed mode,
// not an application-enforced TTL. Archive failures can keep older rows online.
func diagnosticRetentionDays() int {
	if diagnosticArchiveManaged() {
		return 3
	}
	return 30
}
