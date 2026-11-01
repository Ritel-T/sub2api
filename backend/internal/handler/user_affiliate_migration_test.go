//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type migrationHandlerAffiliateRepo struct {
	service.AffiliateRepository
	inviterID    *int64
	boundUser    int64
	boundInviter int64
}

func (r *migrationHandlerAffiliateRepo) EnsureUserAffiliate(_ context.Context, id int64) (*service.AffiliateSummary, error) {
	summary := &service.AffiliateSummary{UserID: id, AffCode: "CURRENT52"}
	if id == 300 {
		summary.InviterID = r.inviterID
	}
	return summary, nil
}
func (r *migrationHandlerAffiliateRepo) GetAffiliateByCode(context.Context, string) (*service.AffiliateSummary, error) {
	return &service.AffiliateSummary{UserID: 52}, nil
}
func (r *migrationHandlerAffiliateRepo) BindInviter(_ context.Context, userID, inviterID int64) (bool, error) {
	r.boundUser, r.boundInviter = userID, inviterID
	r.inviterID = &inviterID
	return true, nil
}

func TestUserAffiliateMigrationHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name          string
		method        string
		authenticated bool
		bound         bool
		body          string
		status        int
		reason        string
	}{
		{name: "get_requires_auth", method: "GET", status: 401},
		{name: "post_requires_auth", method: "POST", body: `{ "inviter_key": "hythyr" }`, status: 401},
		{name: "get_options", method: "GET", authenticated: true, status: 200},
		{name: "get_bound_empty_array", method: "GET", authenticated: true, bound: true, status: 200},
		{name: "post_own_subject_only", method: "POST", authenticated: true, body: `{"inviter_key":"hythyr","user_id":999,"inviter_id":210}`, status: 200},
		{name: "post_bound_conflict", method: "POST", authenticated: true, bound: true, body: `{"inviter_key":"hythyr"}`, status: 409, reason: "AFFILIATE_ALREADY_BOUND"},
		{name: "post_invalid_key", method: "POST", authenticated: true, body: `{"inviter_key":"210"}`, status: 400, reason: "AFFILIATE_INVITER_INVALID"},
		{name: "post_missing_key", method: "POST", authenticated: true, body: `{"inviter_id":52}`, status: 400, reason: "AFFILIATE_INVITER_INVALID"},
		{name: "post_wrong_type", method: "POST", authenticated: true, body: `{"inviter_key":52}`, status: 400, reason: "AFFILIATE_INVITER_INVALID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &migrationHandlerAffiliateRepo{}
			original := int64(127)
			if tc.bound {
				repo.inviterID = &original
			}
			users := &userHandlerRepoStub{user: &service.User{ID: 52, Status: service.StatusActive}}
			h := &UserHandler{
				affiliateService: service.NewAffiliateService(repo, nil, nil, nil),
				userService:      service.NewUserService(users, nil, nil, nil),
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tc.authenticated {
					c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 300})
				}
				c.Next()
			})
			router.GET("/api/v1/user/aff/migration", h.GetAffiliateMigration)
			router.POST("/api/v1/user/aff/migration", h.BindMigrationInviter)
			request := httptest.NewRequest(tc.method, "/api/v1/user/aff/migration", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			var envelope map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			if tc.status != http.StatusOK {
				if tc.reason != "" {
					require.JSONEq(t, `"`+tc.reason+`"`, string(envelope["reason"]))
				}
				require.Zero(t, repo.boundUser)
				if tc.bound {
					require.Equal(t, original, *repo.inviterID)
				}
				return
			}
			require.JSONEq(t, "0", string(envelope["code"]))
			var data map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(envelope["data"], &data))
			require.Len(t, data, 2)
			if tc.bound || tc.method == "POST" {
				require.JSONEq(t, `{"bound":true,"options":[]}`, string(envelope["data"]))
			} else {
				require.JSONEq(t, "false", string(data["bound"]))
				var options []map[string]string
				require.NoError(t, json.Unmarshal(data["options"], &options))
				require.Len(t, options, 8)
				for _, option := range options {
					require.Len(t, option, 2)
					require.Contains(t, option, "key")
					require.Contains(t, option, "label")
				}
			}
			if tc.method == "POST" {
				require.Equal(t, int64(300), repo.boundUser)
				require.Equal(t, int64(52), repo.boundInviter)
			}
		})
	}
}
