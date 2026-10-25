//go:build unit

package service

import "testing"

func TestDiagnosticRetentionMetadata(t *testing.T) {
	for _, v := range []string{"", "false", "true", "TRUE"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("SUB2API_DIAGNOSTIC_ARCHIVE_MANAGED", v)
			days, managed := DiagnosticRetentionMetadata()
			if v == "true" {
				if days != 3 || !managed {
					t.Fatal(days, managed)
				}
			} else if days != 30 || managed {
				t.Fatal(days, managed)
			}
		})
	}
}
