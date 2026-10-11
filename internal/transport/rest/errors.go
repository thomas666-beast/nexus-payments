package rest

import (
	"errors"
	"net/http"

	"github.com/thomas666-beast/nexus-payments/internal/ledger"
	"github.com/thomas666-beast/nexus-payments/internal/service"
	"github.com/thomas666-beast/nexus-payments/internal/store"
)

// errorResponse maps a domain error to an HTTP status code and a wire code.
func errorResponse(err error) (int, ErrorDTO) {
	switch {
	case errors.Is(err, service.ErrInvalidRequest):
		return http.StatusBadRequest, ErrorDTO{Error: err.Error(), Code: "invalid_request"}
	case errors.Is(err, service.ErrIdempotencyConflict):
		return http.StatusConflict, ErrorDTO{Error: err.Error(), Code: "idempotency_conflict"}
	case errors.Is(err, service.ErrIdempotencyInProgress):
		return http.StatusConflict, ErrorDTO{Error: err.Error(), Code: "idempotency_in_progress"}
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, ErrorDTO{Error: err.Error(), Code: "not_found"}
	case errors.Is(err, ledger.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, ErrorDTO{Error: err.Error(), Code: "insufficient_funds"}
	case errors.Is(err, ledger.ErrAccountNotActive):
		return http.StatusUnprocessableEntity, ErrorDTO{Error: err.Error(), Code: "account_not_active"}
	default:
		return http.StatusInternalServerError, ErrorDTO{Error: "internal error", Code: "internal"}
	}
}
