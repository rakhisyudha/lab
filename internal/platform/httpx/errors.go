package httpx

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
)

// ErrorBody adalah isi field `error` pada Error_Envelope (design 2.2).
type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details"`
	RequestID string `json:"request_id"`
}

// ErrorEnvelope adalah bentuk tunggal seluruh response error API (Requirement 9.2).
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// Fail menulis Error_Envelope dan menghentikan rantai handler.
//
// Error yang bukan *apperr.Error, atau yang berstatus 5xx, dicatat ke gin.Context.Errors
// agar access log memuat penyebabnya. Penyebab itu tidak pernah dikirim ke client
// (Requirement 18.7). Error non-apperr dipetakan ke internal_error.
func Fail(c *gin.Context, err error) {
	if err == nil {
		err = errors.New("httpx: Fail dipanggil dengan error nil")
	}
	ae, ok := apperr.As(err)
	if !ok {
		_ = c.Error(err)
		ae = apperr.New(apperr.CodeInternalError)
	} else if ae.HTTPStatus >= 500 {
		_ = c.Error(err)
	}
	c.AbortWithStatusJSON(ae.HTTPStatus, newEnvelope(c, ae))
}

// newEnvelope menyusun envelope. Details nil menjadi objek kosong agar field
// `details` selalu ada pada response.
func newEnvelope(c *gin.Context, ae *apperr.Error) ErrorEnvelope {
	details := ae.Details
	if details == nil {
		details = map[string]any{}
	}
	return ErrorEnvelope{
		Error: ErrorBody{
			Code:      string(ae.Code),
			Message:   ae.ResolvedMessage(),
			Details:   details,
			RequestID: RequestIDFrom(c.Request.Context()),
		},
	}
}

// NotFound adalah handler untuk rute yang tidak terdaftar. Response memakai
// Error_Envelope, bukan halaman teks bawaan Gin.
func NotFound() gin.HandlerFunc {
	return func(c *gin.Context) {
		Fail(c, apperr.New(apperr.CodeNotFound))
	}
}
