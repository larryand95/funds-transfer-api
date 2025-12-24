package handlers

import (
	"net/http"
	"time"

	"github.com/array/banking-api/internal/dto"
	"github.com/array/banking-api/internal/errors"
	"github.com/array/banking-api/internal/services"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// ExternalAccountHandler handles external account-related HTTP requests
type ExternalAccountHandler struct {
	externalAccountService *services.ExternalAccountService
}

// NewExternalAccountHandler creates a new external account handler
func NewExternalAccountHandler(externalAccountService *services.ExternalAccountService) *ExternalAccountHandler {
	return &ExternalAccountHandler{
		externalAccountService: externalAccountService,
	}
}

// RegisterExternalAccount registers a new external NorthWind account
// @Summary Register external account
// @Description Register a new NorthWind Bank account for transfers
// @Tags External Accounts
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body dto.RegisterExternalAccountRequest true "External account details"
// @Success 201 {object} dto.RegisterExternalAccountResponse "External account registered successfully"
// @Failure 400 {object} errors.ErrorResponse "Invalid request"
// @Failure 401 {object} errors.ErrorResponse "Unauthorized"
// @Failure 409 {object} errors.ErrorResponse "Account already exists"
// @Failure 500 {object} errors.ErrorResponse "Internal server error"
// @Router /external-accounts [post]
func (h *ExternalAccountHandler) RegisterExternalAccount(c echo.Context) error {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		return SendError(c, errors.AuthMissingToken)
	}

	var req dto.RegisterExternalAccountRequest
	if err := c.Bind(&req); err != nil {
		return SendError(c, errors.ValidationGeneral, errors.WithDetails("Invalid request body"))
	}

	if err := c.Validate(req); err != nil {
		return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
	}

	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()

	externalAccount, err := h.externalAccountService.RegisterExternalAccount(userID, req, ipAddress, userAgent)
	if err != nil {
		if err == services.ErrExternalAccountAlreadyExists {
			return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
		}
		return SendSystemError(c, err)
	}

	return c.JSON(http.StatusCreated, dto.RegisterExternalAccountResponse{
		ID:            externalAccount.ID,
		AccountNumber: externalAccount.AccountNumber,
		AccountName:   externalAccount.AccountName,
		BankName:      externalAccount.BankName,
		Currency:      externalAccount.Currency,
		Status:        externalAccount.Status,
		IsVerified:    externalAccount.IsVerified(),
		Nickname:      externalAccount.Nickname,
		CreatedAt:     externalAccount.CreatedAt.Format(time.RFC3339),
	})
}

// GetExternalAccounts retrieves all external accounts for the authenticated user
// @Summary Get external accounts
// @Description Retrieve all registered NorthWind Bank accounts for the authenticated user
// @Tags External Accounts
// @Security BearerAuth
// @Produce json
// @Success 200 {array} dto.ExternalAccountResponse "List of external accounts"
// @Failure 401 {object} errors.ErrorResponse "Unauthorized"
// @Failure 500 {object} errors.ErrorResponse "Internal server error"
// @Router /external-accounts [get]
func (h *ExternalAccountHandler) GetExternalAccounts(c echo.Context) error {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		return SendError(c, errors.AuthMissingToken)
	}

	accounts, err := h.externalAccountService.GetExternalAccounts(userID)
	if err != nil {
		return SendSystemError(c, err)
	}

	responses := make([]dto.ExternalAccountResponse, len(accounts))
	for i, account := range accounts {
		responses[i] = dto.ExternalAccountResponse{
			ID:                account.ID,
			AccountNumber:     account.AccountNumber,
			AccountName:       account.AccountName,
			BankName:          account.BankName,
			BankCode:          account.BankCode,
			Currency:          account.Currency,
			Status:            account.Status,
			IsVerified:        account.IsVerified(),
			VerificationError: getStringValue(account.VerificationError),
			Nickname:          account.Nickname,
			CreatedAt:         account.CreatedAt.Format(time.RFC3339),
		}
		if account.VerifiedAt != nil {
			responses[i].VerifiedAt = account.VerifiedAt.Format(time.RFC3339)
		}
	}

	return c.JSON(http.StatusOK, responses)
}

// GetExternalAccount retrieves a specific external account
// @Summary Get external account by ID
// @Description Retrieve details of a specific external account
// @Tags External Accounts
// @Security BearerAuth
// @Produce json
// @Param accountId path string true "External Account ID (UUID)"
// @Success 200 {object} dto.ExternalAccountResponse "External account details"
// @Failure 400 {object} errors.ErrorResponse "Invalid account ID"
// @Failure 401 {object} errors.ErrorResponse "Unauthorized"
// @Failure 403 {object} errors.ErrorResponse "Account belongs to another user"
// @Failure 404 {object} errors.ErrorResponse "Account not found"
// @Failure 500 {object} errors.ErrorResponse "Internal server error"
// @Router /external-accounts/{accountId} [get]
func (h *ExternalAccountHandler) GetExternalAccount(c echo.Context) error {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		return SendError(c, errors.AuthMissingToken)
	}

	accountID, err := uuid.Parse(c.Param("accountId"))
	if err != nil {
		return SendError(c, errors.ValidationGeneral, errors.WithDetails("Invalid account ID format"))
	}

	account, err := h.externalAccountService.GetExternalAccount(userID, accountID)
	if err != nil {
		if err == services.ErrExternalAccountNotFound {
			return SendError(c, errors.AccountNotFound, errors.WithDetails(err.Error()))
		}
		if err == services.ErrUnauthorized {
			return SendError(c, errors.AuthInsufficientPermission, errors.WithDetails(err.Error()))
		}
		return SendSystemError(c, err)
	}

	response := dto.ExternalAccountResponse{
		ID:                account.ID,
		AccountNumber:     account.AccountNumber,
		AccountName:       account.AccountName,
		BankName:          account.BankName,
		BankCode:          account.BankCode,
		Currency:          account.Currency,
		Status:            account.Status,
		IsVerified:        account.IsVerified(),
		VerificationError: getStringValue(account.VerificationError),
		Nickname:          account.Nickname,
		CreatedAt:         account.CreatedAt.Format(time.RFC3339),
	}
	if account.VerifiedAt != nil {
		response.VerifiedAt = account.VerifiedAt.Format(time.RFC3339)
	}

	return c.JSON(http.StatusOK, response)
}

// DeleteExternalAccount deletes an external account
// @Summary Delete external account
// @Description Delete a registered external account
// @Tags External Accounts
// @Security BearerAuth
// @Produce json
// @Param accountId path string true "External Account ID (UUID)"
// @Success 204 "No Content"
// @Failure 400 {object} errors.ErrorResponse "Invalid account ID"
// @Failure 401 {object} errors.ErrorResponse "Unauthorized"
// @Failure 403 {object} errors.ErrorResponse "Account belongs to another user"
// @Failure 404 {object} errors.ErrorResponse "Account not found"
// @Failure 500 {object} errors.ErrorResponse "Internal server error"
// @Router /external-accounts/{accountId} [delete]
func (h *ExternalAccountHandler) DeleteExternalAccount(c echo.Context) error {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		return SendError(c, errors.AuthMissingToken)
	}

	accountID, err := uuid.Parse(c.Param("accountId"))
	if err != nil {
		return SendError(c, errors.ValidationGeneral, errors.WithDetails("Invalid account ID format"))
	}

	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()

	err = h.externalAccountService.DeleteExternalAccount(userID, accountID, ipAddress, userAgent)
	if err != nil {
		if err == services.ErrExternalAccountNotFound {
			return SendError(c, errors.AccountNotFound, errors.WithDetails(err.Error()))
		}
		if err == services.ErrUnauthorized {
			return SendError(c, errors.AuthInsufficientPermission, errors.WithDetails(err.Error()))
		}
		return SendSystemError(c, err)
	}

	return c.NoContent(http.StatusNoContent)
}

// Helper function to get string value from pointer
func getStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

