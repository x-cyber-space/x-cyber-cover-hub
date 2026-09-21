package api

import "net/http"

// ErrorResponse is the error envelope, matching the sibling lyrics service so
// the two APIs read the same way.
type ErrorResponse struct {
	Message    string `json:"message"`
	Name       string `json:"name"`
	StatusCode int    `json:"statusCode"`
}

// Error names used by this API.
const (
	ErrNameBadRequest    = "BadRequest"
	ErrNameCoverNotFound = "CoverNotFound"
	ErrNameUpstream      = "UpstreamUnavailable"
)

// CoverNotFoundError is returned when no source offered artwork for an album it
// could confidently identify.
func CoverNotFoundError() ErrorResponse {
	return ErrorResponse{
		Message:    "No artwork found for the requested album",
		Name:       ErrNameCoverNotFound,
		StatusCode: http.StatusNotFound,
	}
}

// BadRequestError is returned for a request that cannot be interpreted.
func BadRequestError(message string) ErrorResponse {
	return ErrorResponse{
		Message:    message,
		Name:       ErrNameBadRequest,
		StatusCode: http.StatusBadRequest,
	}
}

// UpstreamError is returned when every artwork source failed, which is an
// outage rather than a missing cover.
func UpstreamError() ErrorResponse {
	return ErrorResponse{
		Message:    "All artwork providers are unavailable",
		Name:       ErrNameUpstream,
		StatusCode: http.StatusServiceUnavailable,
	}
}
