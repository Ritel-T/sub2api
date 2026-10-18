package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionOverflowPolicyIntersectsBothScopes(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		sourceAllow, targetAllow   []string
		denied                     []string
		sourceStream, targetStream bool
		body                       string
		reason                     string
	}{
		{name: "both allow", sourceAllow: []string{"gpt-6-*"}, targetAllow: []string{"gpt-6-sol"}, body: `{"model":"gpt-6-sol","stream":true}`},
		{name: "source deny", sourceAllow: []string{"gpt-6-luna"}, targetAllow: []string{"gpt-6-sol"}, body: `{"model":"gpt-6-sol"}`, reason: "MODEL_NOT_ALLOWED"},
		{name: "target deny", sourceAllow: []string{"gpt-6-sol"}, targetAllow: []string{"gpt-6-luna"}, body: `{"model":"gpt-6-sol"}`, reason: "MODEL_NOT_ALLOWED"},
		{name: "user deny", denied: []string{"gpt-6-*"}, body: `{"model":"gpt-6-sol"}`, reason: "MODEL_NOT_ALLOWED"},
		{name: "duplicate cannot bypass", sourceAllow: []string{"gpt-6-sol"}, body: `{"model":"gpt-6-sol","Model":"gpt-6-luna"}`, reason: "MODEL_NOT_ALLOWED"},
		{name: "source stream required", sourceStream: true, body: `{"model":"gpt-6-sol"}`, reason: "STREAM_REQUIRED"},
		{name: "target stream required", targetStream: true, body: `{"model":"gpt-6-sol","stream":false}`, reason: "STREAM_REQUIRED"},
		{name: "stream duplicate cannot bypass", targetStream: true, body: `{"model":"gpt-6-sol","stream":true,"Stream":false}`, reason: "STREAM_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &service.Group{ID: 1, Platform: service.PlatformOpenAI, StreamOnly: tc.sourceStream, ModelAllowlist: service.GroupModelAllowlist{Enabled: tc.sourceAllow != nil, Models: tc.sourceAllow}}
			target := &service.Group{ID: 2, Platform: service.PlatformOpenAI, StreamOnly: tc.targetStream, ModelAllowlist: service.GroupModelAllowlist{Enabled: tc.targetAllow != nil, Models: tc.targetAllow}}
			key := &service.APIKey{Group: target, User: &service.User{UserGroupDeniedModels: tc.denied}, SubscriptionOverflow: &service.SubscriptionOverflowEvidence{SourceGroup: source}}
			rec := httptest.NewRecorder()
			router := gin.New()
			router.POST("/v1/responses", func(c *gin.Context) {
				err := ValidateSubscriptionOverflowRequestPolicy(c, key, []byte(tc.body))
				if tc.reason == "" {
					require.NoError(t, err)
				} else {
					require.Equal(t, tc.reason, infraerrors.Reason(err))
				}
				require.False(t, c.Writer.Written(), "policy must preserve status/error envelope ownership")
			})
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(tc.body)))
		})
	}
}

func TestSubscriptionOverflowImagePermissionRequiresBothGroups(t *testing.T) {
	for _, sourceAllows := range []bool{false, true} {
		for _, targetAllows := range []bool{false, true} {
			source := &service.Group{ID: 1, Platform: service.PlatformOpenAI, AllowImageGeneration: sourceAllows}
			target := &service.Group{ID: 2, Platform: service.PlatformOpenAI, AllowImageGeneration: targetAllows}
			key := &service.APIKey{Group: target, User: &service.User{}, SubscriptionOverflow: &service.SubscriptionOverflowEvidence{SourceGroup: source}}
			router := gin.New()
			router.POST("/v1/images/generations", func(c *gin.Context) {
				err := ValidateSubscriptionOverflowRequestPolicy(c, key, []byte(`{"model":"gpt-image-1"}`))
				if sourceAllows && targetAllows {
					require.NoError(t, err)
				} else {
					require.Equal(t, "IMAGE_GENERATION_NOT_ALLOWED", infraerrors.Reason(err))
				}
			})
			router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil))
		}
	}
}
