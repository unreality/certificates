package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi"
	"github.com/smallstep/certificates/acme"
	"github.com/smallstep/certificates/api"
	"github.com/smallstep/certificates/logging"
)

// NewAccountRequest represents the payload for a new account request.
type NewAccountRequest struct {
	Contact              []string `json:"contact"`
	OnlyReturnExisting   bool     `json:"onlyReturnExisting"`
	TermsOfServiceAgreed bool     `json:"termsOfServiceAgreed"`
}

func validateContacts(cs []string) error {
	for _, c := range cs {
		if len(c) == 0 {
			return acme.NewError(acme.ErrorMalformedType, "contact cannot be empty string")
		}
	}
	return nil
}

// Validate validates a new-account request body.
func (n *NewAccountRequest) Validate() error {
	if n.OnlyReturnExisting && len(n.Contact) > 0 {
		return acme.NewError(acme.ErrorMalformedType, "incompatible input; onlyReturnExisting must be alone")
	}
	return validateContacts(n.Contact)
}

// UpdateAccountRequest represents an update-account request.
type UpdateAccountRequest struct {
	Contact []string    `json:"contact"`
	Status  acme.Status `json:"status"`
}

// Validate validates a update-account request body.
func (u *UpdateAccountRequest) Validate() error {
	switch {
	case len(u.Status) > 0 && len(u.Contact) > 0:
		return acme.NewError(acme.ErrorMalformedType, "incompatible input; contact and "+
			"status updates are mutually exclusive")
	case len(u.Contact) > 0:
		if err := validateContacts(u.Contact); err != nil {
			return err
		}
		return nil
	case len(u.Status) > 0:
		if u.Status != acme.StatusDeactivated {
			return acme.NewError(acme.ErrorMalformedType, "cannot update account "+
				"status to %s, only deactivated", u.Status)
		}
		return nil
	default:
		// According to the ACME spec (https://tools.ietf.org/html/rfc8555#section-7.3.2)
		// accountUpdate should ignore any fields not recognized by the server.
		return nil
	}
}

// NewAccount is the handler resource for creating new ACME accounts.
func (h *Handler) NewAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload, err := payloadFromContext(ctx)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	var nar NewAccountRequest
	if err := json.Unmarshal(payload.value, &nar); err != nil {
		api.WriteError(w, acme.WrapError(acme.ErrorMalformedType, err,
			"failed to unmarshal new-account request payload"))
		return
	}
	if err := nar.Validate(); err != nil {
		api.WriteError(w, err)
		return
	}

	httpStatus := http.StatusCreated
	acc, err := accountFromContext(r.Context())
	if err != nil {
		acmeErr, ok := err.(*acme.Error)
		if !ok || acmeErr.Status != http.StatusBadRequest {
			// Something went wrong ...
			api.WriteError(w, err)
			return
		}

		// Account does not exist //
		if nar.OnlyReturnExisting {
			api.WriteError(w, acme.NewError(acme.ErrorAccountDoesNotExistType,
				"account does not exist"))
			return
		}
		jwk, err := jwkFromContext(ctx)
		if err != nil {
			api.WriteError(w, err)
			return
		}

		acc = &acme.Account{
			Key:     jwk,
			Contact: nar.Contact,
			Status:  acme.StatusValid,
		}
		if err := h.db.CreateAccount(ctx, acc); err != nil {
			api.WriteError(w, acme.WrapErrorISE(err, "error creating account"))
			return
		}
	} else {
		// Account exists //
		httpStatus = http.StatusOK
	}

	h.linker.LinkAccount(ctx, acc)

	w.Header().Set("Location", h.linker.GetLink(r.Context(), AccountLinkType,
		true, acc.ID))
	api.JSONStatus(w, acc, httpStatus)
}

// GetUpdateAccount is the api for updating an ACME account.
func (h *Handler) GetUpdateAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	acc, err := accountFromContext(ctx)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	payload, err := payloadFromContext(ctx)
	if err != nil {
		api.WriteError(w, err)
		return
	}

	// If PostAsGet just respond with the account, otherwise process like a
	// normal Post request.
	if !payload.isPostAsGet {
		var uar UpdateAccountRequest
		if err := json.Unmarshal(payload.value, &uar); err != nil {
			api.WriteError(w, acme.WrapError(acme.ErrorMalformedType, err,
				"failed to unmarshal new-account request payload"))
			return
		}
		if err := uar.Validate(); err != nil {
			api.WriteError(w, err)
			return
		}
		if len(uar.Status) > 0 || len(uar.Contact) > 0 {
			if len(uar.Status) > 0 {
				acc.Status = uar.Status
			} else if len(uar.Contact) > 0 {
				acc.Contact = uar.Contact
			}

			if err := h.db.UpdateAccount(ctx, acc); err != nil {
				api.WriteError(w, acme.WrapErrorISE(err, "error updating account"))
				return
			}
		}
	}

	h.linker.LinkAccount(ctx, acc)

	w.Header().Set("Location", h.linker.GetLink(ctx, AccountLinkType, true, acc.ID))
	api.JSON(w, acc)
}

func logOrdersByAccount(w http.ResponseWriter, oids []string) {
	if rl, ok := w.(logging.ResponseLogger); ok {
		m := map[string]interface{}{
			"orders": oids,
		}
		rl.WithFields(m)
	}
}

// GetOrdersByAccountID ACME api for retrieving the list of order urls belonging to an account.
func (h *Handler) GetOrdersByAccountID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	acc, err := accountFromContext(ctx)
	if err != nil {
		api.WriteError(w, err)
		return
	}
	accID := chi.URLParam(r, "accID")
	if acc.ID != accID {
		api.WriteError(w, acme.NewError(acme.ErrorUnauthorizedType, "account ID '%s' does not match url param '%s'", acc.ID, accID))
		return
	}
	orders, err := h.db.GetOrdersByAccountID(ctx, acc.ID)
	if err != nil {
		api.WriteError(w, err)
		return
	}

	h.linker.LinkOrdersByAccountID(ctx, orders)

	api.JSON(w, orders)
	logOrdersByAccount(w, orders)
}
