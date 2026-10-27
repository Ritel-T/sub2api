package logredact

import (
	"strings"
	"testing"
)

func TestRedactText_AccountSecurityCredentials(t *testing.T) {
	body := `{"old_password":"old-canary","new_password":"new-canary","password":"pwd-canary","verify_code":"code-canary","data":{"access_token":"access-canary","refresh_token":"refresh-canary"}}`
	for _, input := range []string{body, "PUT /api/v1/user/account-security/password " + body, "POST /api/v1/user/account-security/email old_password=old-canary new_password=new-canary password=pwd-canary verify_code=code-canary access_token=access-canary refresh_token=refresh-canary"} {
		out := RedactText(input)
		for _, secret := range []string{"old-canary", "new-canary", "pwd-canary", "code-canary", "access-canary", "refresh-canary"} {
			if strings.Contains(out, secret) {
				t.Fatal("account credential value escaped path-independent redaction")
			}
		}
	}
}
