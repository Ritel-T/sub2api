package antigravity

import (
	"encoding/json"

	"github.com/google/uuid"
)

func BuildV1InternalEnvelope(projectID, model, requestType string, request GeminiRequest) ([]byte, error) {
	v1Req := V1InternalRequest{
		Project:     projectID,
		RequestID:   "agent-" + uuid.NewString(),
		UserAgent:   "antigravity",
		RequestType: requestType,
		Model:       model,
		Request:     request,
	}
	return json.Marshal(v1Req)
}
