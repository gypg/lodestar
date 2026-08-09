package op

import (
	"net/http"

	"github.com/gypg/lodestar/internal/apperror"
)

const (
	CodeSiteImportInvalidJSON           = "site.import.invalid_json"
	CodeSiteImportEmptyPayload          = "site.import.empty_payload"
	CodeSiteImportTooLarge              = "site.import.too_large"
	CodeSiteImportUnrecognizedAllAPIHub = "site.import.unrecognized_all_api_hub"
	CodeSiteImportUnrecognizedMetapi    = "site.import.unrecognized_metapi"
	CodeSiteImportNoImportableAllAPIHub = "site.import.no_importable_all_api_hub"
	CodeSiteImportNoImportableMetapi    = "site.import.no_importable_metapi"
	CodeSiteImportUnsupportedPayload    = "site.import.unsupported_payload"
	CodeSiteImportPersistFailed         = "site.import.persist_failed"
)

func newSiteImportInvalidJSONError() *apperror.Error {
	return apperror.New(CodeSiteImportInvalidJSON, "site import invalid json").WithStatus(http.StatusBadRequest)
}

func newSiteImportEmptyPayloadError() *apperror.Error {
	return apperror.New(CodeSiteImportEmptyPayload, "site import empty payload").WithStatus(http.StatusBadRequest)
}

// NewSiteImportTooLargeError 报告导入体超过大小上限。导出给 handler 层用，
// 因为体积闸门必须在读入 body 之前生效，而 body 尚未到达 op 层。
func NewSiteImportTooLargeError(limit string) *apperror.Error {
	return apperror.Newf(CodeSiteImportTooLarge, "site import payload exceeds %s limit", limit).
		WithStatus(http.StatusRequestEntityTooLarge).
		WithParam("limit", limit)
}

func newSiteImportUnrecognizedAllAPIHubError() *apperror.Error {
	return apperror.New(CodeSiteImportUnrecognizedAllAPIHub, "site import no recognizable all api hub payload sections").WithStatus(http.StatusBadRequest)
}

func newSiteImportUnrecognizedMetapiError() *apperror.Error {
	return apperror.New(CodeSiteImportUnrecognizedMetapi, "site import no recognizable metapi accounts section").WithStatus(http.StatusBadRequest)
}

func newSiteImportNoImportableAllAPIHubError() *apperror.Error {
	return apperror.New(CodeSiteImportNoImportableAllAPIHub, "site import no importable all api hub site account data").WithStatus(http.StatusBadRequest)
}

func newSiteImportNoImportableMetapiError() *apperror.Error {
	return apperror.New(CodeSiteImportNoImportableMetapi, "site import no importable metapi site account data").WithStatus(http.StatusBadRequest)
}

func newSiteImportUnsupportedPayloadError(message string) *apperror.Error {
	if message == "" {
		message = "site import unsupported payload"
	}
	return apperror.New(CodeSiteImportUnsupportedPayload, message).WithStatus(http.StatusBadRequest)
}

func wrapSiteImportPersistFailedError(err error) *apperror.Error {
	return apperror.Wrap(CodeSiteImportPersistFailed, "site import persist failed", err).WithStatus(http.StatusInternalServerError)
}
