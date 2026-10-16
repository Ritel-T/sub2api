package service

import (
	"net/mail"
	"strings"
)

// rewriteSMTPRecipientDomain runs after identity/OTP/template processing and
// before SMTP construction. Only the parsed recipient's exact domain changes;
// local part, display name, original caller string and message body are retained.
func rewriteSMTPRecipientDomain(to string, aliases map[string]string) (string, error) {
	if len(aliases) == 0 {
		return to, nil
	}
	address, err := parseSMTPAddress(to, "recipient")
	if err != nil {
		return "", err
	}
	at := strings.LastIndexByte(address.Address, '@')
	if at < 0 {
		return to, nil
	}
	destination, mapped := aliases[strings.ToLower(address.Address[at+1:])]
	if !mapped {
		return to, nil
	}
	return (&mail.Address{Name: address.Name, Address: address.Address[:at+1] + destination}).String(), nil
}
