package handlers

import (
	"net/http"
	"time"

	"github.com/array/banking-api/internal/dto"
	"github.com/array/banking-api/internal/errors"
	"github.com/array/banking-api/internal/models"
	"github.com/array/banking-api/internal/repositories"
	"github.com/array/banking-api/internal/services"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// ExternalTransferHandler handles external transfer-related HTTP requests
type ExternalTransferHandler struct {
	transferService *services.NorthWindTransferService
}

// NewExternalTransferHandler creates a new external transfer handler
func NewExternalTransferHandler(transferService *services.NorthWindTransferService) *ExternalTransferHandler {
	return &ExternalTransferHandler{
		transferService: transferService,
	}
}

// InitiateExternalTransfer initiates a transfer to a NorthWind account
// @Summary Initiate external transfer
// @Description Initiate a transfer to a registered NorthWind Bank account
// @Tags External Transfers
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param request body dto.InitiateExternalTransferRequest true "Transfer details"
// @Success 201 {object} dto.InitiateExternalTransferResponse "Transfer initiated successfully"
// @Failure 400 {object} errors.ErrorResponse "Invalid request"
// @Failure 401 {object} errors.ErrorResponse "Unauthorized"
// @Failure 403 {object} errors.ErrorResponse "Insufficient funds or account not active"
// @Failure 404 {object} errors.ErrorResponse "Account not found"
// @Failure 409 {object} errors.ErrorResponse "Transfer already in progress"
// @Failure 500 {object} errors.ErrorResponse "Internal server error"
// @Router /external-transfers [post]
func (h *ExternalTransferHandler) InitiateExternalTransfer(c echo.Context) error {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		return SendError(c, errors.AuthMissingToken)
	}

	var req dto.InitiateExternalTransferRequest
	if err := c.Bind(&req); err != nil {
		return SendError(c, errors.ValidationGeneral, errors.WithDetails("Invalid request body"))
	}

	if err := c.Validate(req); err != nil {
		return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
	}

	ipAddress := c.RealIP()
	userAgent := c.Request().UserAgent()

	transfer, err := h.transferService.InitiateExternalTransfer(userID, req, ipAddress, userAgent)
	if err != nil {
		if err == services.ErrAccountNotFound {
			return SendError(c, errors.AccountNotFound, errors.WithDetails(err.Error()))
		}
		if err == services.ErrExternalAccountNotFound {
			return SendError(c, errors.AccountNotFound, errors.WithDetails(err.Error()))
		}
		if err == services.ErrUnauthorized {
			return SendError(c, errors.AuthInsufficientPermission, errors.WithDetails(err.Error()))
		}
		if err == services.ErrInsufficientFunds {
			return SendError(c, errors.TransactionInsufficientFunds, errors.WithDetails(err.Error()))
		}
		if err == services.ErrAccountNotActive {
			return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
		}
		if err == services.ErrExternalAccountNotActive {
			return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
		}
		if err == services.ErrInvalidTransferType {
			return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
		}
		if err == services.ErrScheduledTimeRequired {
			return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
		}
		if err == services.ErrScheduledTimeInPast {
			return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
		}
		if err == services.ErrTransferPending {
			return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
		}
		if err == services.ErrTransferFailed {
			return SendError(c, errors.ValidationGeneral, errors.WithDetails(err.Error()))
		}
		return SendSystemError(c, err)
	}

	response := dto.InitiateExternalTransferResponse{
		TransferID:   transfer.ID,
		Status:        transfer.Status,
		Amount:        transfer.Amount.String(),
		Currency:      "USD", // Default, should be from account
		TransferType:  transfer.TransferType,
		CreatedAt:     transfer.CreatedAt.Format(time.RFC3339),
	}

	if transfer.NorthWindTransferID != nil {
		response.NorthWindTransferID = *transfer.NorthWindTransferID
	}

	if transfer.EstimatedCompletion != nil {
		response.EstimatedCompletion = transfer.EstimatedCompletion.Format(time.RFC3339)
	}

	if transfer.Status == models.TransferStatusPending && transfer.TransferType == models.TransferTypeScheduled {
		response.Message = "Transfer scheduled successfully"
	} else {
		response.Message = "Transfer initiated successfully"
	}

	return c.JSON(http.StatusCreated, response)
}

// GetTransferStatus checks the status of an external transfer
// @Summary Get transfer status
// @Description Check the current status of an external transfer with NorthWind
// @Tags External Transfers
// @Security BearerAuth
// @Produce json
// @Param transferId path string true "Transfer ID (UUID)"
// @Success 200 {object} dto.ExternalTransferStatusResponse "Transfer status"
// @Failure 400 {object} errors.ErrorResponse "Invalid transfer ID"
// @Failure 401 {object} errors.ErrorResponse "Unauthorized"
// @Failure 404 {object} errors.ErrorResponse "Transfer not found"
// @Failure 500 {object} errors.ErrorResponse "Internal server error"
// @Router /external-transfers/{transferId}/status [get]
func (h *ExternalTransferHandler) GetTransferStatus(c echo.Context) error {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		return SendError(c, errors.AuthMissingToken)
	}

	transferID, err := uuid.Parse(c.Param("transferId"))
	if err != nil {
		return SendError(c, errors.ValidationGeneral, errors.WithDetails("Invalid transfer ID format"))
	}

	transfer, err := h.transferService.CheckTransferStatus(transferID)
	if err != nil {
		if err == repositories.ErrTransferNotFound {
			return SendError(c, errors.TransferNotFound, errors.WithDetails(err.Error()))
		}
		return SendSystemError(c, err)
	}

	// TODO: Verify ownership (user must own the from account)
	// This should be done in the service layer
	_ = userID // Suppress unused variable warning for now

	response := dto.ExternalTransferStatusResponse{
		TransferID:   transfer.ID,
		Status:        transfer.Status,
		Amount:        transfer.Amount.String(),
		Currency:      "USD", // Should get from account
		TransferType:  transfer.TransferType,
		CreatedAt:     transfer.CreatedAt.Format(time.RFC3339),
	}

	if transfer.NorthWindTransferID != nil {
		response.NorthWindTransferID = *transfer.NorthWindTransferID
	}

	if transfer.EstimatedCompletion != nil {
		response.EstimatedCompletion = transfer.EstimatedCompletion.Format(time.RFC3339)
	}

	if transfer.CompletedAt != nil {
		response.CompletedAt = transfer.CompletedAt.Format(time.RFC3339)
	}

	if transfer.FailedAt != nil {
		response.FailedAt = transfer.FailedAt.Format(time.RFC3339)
		if transfer.ErrorMessage != nil {
			response.FailureReason = *transfer.ErrorMessage
		}
	}

	if transfer.LastStatusCheck != nil {
		response.LastStatusCheck = transfer.LastStatusCheck.Format(time.RFC3339)
	}

	// Calculate progress based on status
	if transfer.Status == models.TransferStatusProcessing {
		// Estimate progress based on time elapsed and estimated completion
		if transfer.EstimatedCompletion != nil {
			now := time.Now()
			totalDuration := transfer.EstimatedCompletion.Sub(transfer.CreatedAt)
			elapsed := now.Sub(transfer.CreatedAt)
			if totalDuration > 0 {
				progress := int((elapsed.Seconds() / totalDuration.Seconds()) * 100)
				if progress > 100 {
					progress = 100
				}
				if progress < 0 {
					progress = 0
				}
				response.Progress = progress
			}
		}
	} else if transfer.Status == models.TransferStatusCompleted {
		response.Progress = 100
	} else if transfer.Status == models.TransferStatusFailed {
		response.Progress = 0
	}

	return c.JSON(http.StatusOK, response)
}

