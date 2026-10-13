package service

import (
	"context"
	"errors"
	"strings"
)

type astraQuestion struct {
	Prompt, Expected string
	HTML             bool
}

func evaluateAstraAnswer(question astraQuestion, answer string) (string, error) {
	answer = strings.TrimSpace(answer)
	if len(answer) > 128<<10 {
		return "", errors.New("test_output_too_large")
	}
	if question.HTML {
		if !pelicanHTMLPattern.MatchString(answer) {
			return answer, errors.New("html_missing")
		}
		return answer, nil
	}
	if answer == "" || (question.Expected != "" && answer != question.Expected) {
		return answer, errors.New("answer_mismatch")
	}
	return answer, nil
}

// Only controlled lightweight source acquisition may collect without candy scoring.
type astraSourceAcquisitionKey struct{}

func WithAstraSourceAcquisition(ctx context.Context) context.Context {
	return context.WithValue(ctx, astraSourceAcquisitionKey{}, true)
}

type astraSourceAcquisitionScope struct {
	accountID int64
}

// A source request has its own native credential, egress and admission. Do not
// apply the target's required-model scope or final-send hook to this child.
// The transport checks this exact account ID against the live source settings.
func WithAstraSourceAcquisitionForAccount(ctx context.Context, id int64) context.Context {
	ctx = context.WithValue(ctx, astraSourceAcquisitionKey{}, astraSourceAcquisitionScope{accountID: id})
	ctx = context.WithValue(ctx, gatewayBorrowRequiredModelContextKey{}, nil)
	return context.WithValue(ctx, gatewayBorrowFinalSendCheckKey{}, nil)
}

func AstraSourceAcquisitionAccountFromContext(ctx context.Context) (int64, bool) {
	scope, bound := ctx.Value(astraSourceAcquisitionKey{}).(astraSourceAcquisitionScope)
	return scope.accountID, bound
}

func IsAstraSourceAcquisition(ctx context.Context) bool {
	if _, bound := AstraSourceAcquisitionAccountFromContext(ctx); bound {
		return true
	}
	v, _ := ctx.Value(astraSourceAcquisitionKey{}).(bool)
	return v
}
