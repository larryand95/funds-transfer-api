package services

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/array/banking-api/internal/dto"
	"github.com/array/banking-api/internal/models"
	"github.com/array/banking-api/internal/repositories"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrInvalidTransferType      = errors.New("invalid transfer type")
	ErrScheduledTimeRequired    = errors.New("scheduled_at is required for scheduled transfers")
	ErrScheduledTimeInPast      = errors.New("scheduled_at must be in the future")
	ErrExternalAccountNotActive = errors.New("external account is not active")
)

// NorthWindTransferService handles transfers to NorthWind Bank
type NorthWindTransferService struct {
	transferRepo        repositories.TransferRepositoryInterface
	accountRepo         repositories.AccountRepositoryInterface
	externalAccountRepo repositories.ExternalAccountRepositoryInterface
	transactionRepo     repositories.TransactionRepositoryInterface
	northWindService    NorthWindServiceInterface
	auditService        AuditServiceInterface
	logger              *slog.Logger
}

// NewNorthWindTransferService creates a new NorthWind transfer service
func NewNorthWindTransferService(
	transferRepo repositories.TransferRepositoryInterface,
	accountRepo repositories.AccountRepositoryInterface,
	externalAccountRepo repositories.ExternalAccountRepositoryInterface,
	transactionRepo repositories.TransactionRepositoryInterface,
	northWindService NorthWindServiceInterface,
	auditService AuditServiceInterface,
	logger *slog.Logger,
) *NorthWindTransferService {
	if logger == nil {
		logger = slog.Default()
	}

	return &NorthWindTransferService{
		transferRepo:        transferRepo,
		accountRepo:         accountRepo,
		externalAccountRepo: externalAccountRepo,
		transactionRepo:     transactionRepo,
		northWindService:    northWindService,
		auditService:        auditService,
		logger:              logger,
	}
}

// InitiateExternalTransfer initiates a transfer to a NorthWind account
func (s *NorthWindTransferService) InitiateExternalTransfer(
	userID uuid.UUID,
	req dto.InitiateExternalTransferRequest,
	ipAddress, userAgent string,
) (*models.Transfer, error) {
	// Parse amount
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}

	if amount.LessThanOrEqual(decimal.Zero) {
		return nil, ErrInvalidAmount
	}

	// Verify from account exists and belongs to user
	fromAccount, err := s.accountRepo.GetByID(req.FromAccountID)
	if err != nil {
		if errors.Is(err, repositories.ErrAccountNotFound) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("failed to get from account: %w", err)
	}

	if fromAccount.UserID != userID {
		return nil, ErrUnauthorized
	}

	if fromAccount.Status != models.AccountStatusActive {
		return nil, ErrAccountNotActive
	}

	// Check sufficient balance
	if fromAccount.Balance.LessThan(amount) {
		return nil, ErrInsufficientFunds
	}

	// Verify external account exists and belongs to user
	externalAccount, err := s.externalAccountRepo.FindByID(req.ExternalAccountID)
	if err != nil {
		if errors.Is(err, repositories.ErrExternalAccountNotFound) {
			return nil, ErrExternalAccountNotFound
		}
		return nil, fmt.Errorf("failed to get external account: %w", err)
	}

	if externalAccount.UserID != userID {
		return nil, ErrUnauthorized
	}

	if !externalAccount.IsActive() {
		return nil, ErrExternalAccountNotActive
	}

	// Validate transfer type
	if !models.IsValidTransferType(req.TransferType) {
		return nil, ErrInvalidTransferType
	}

	// Validate scheduled transfer
	var scheduledAt *time.Time
	if req.TransferType == models.TransferTypeScheduled {
		if req.ScheduledAt == "" {
			return nil, ErrScheduledTimeRequired
		}

		parsedTime, err := time.Parse(time.RFC3339, req.ScheduledAt)
		if err != nil {
			return nil, fmt.Errorf("invalid scheduled_at format: %w", err)
		}

		if parsedTime.Before(time.Now()) {
			return nil, ErrScheduledTimeInPast
		}

		scheduledAt = &parsedTime
	}

	// Set currency
	currency := req.Currency
	if currency == "" {
		currency = fromAccount.Currency
	}

	// Generate idempotency key
	idempotencyKey := fmt.Sprintf("%s-%s-%d", userID.String(), req.ExternalAccountID.String(), time.Now().UnixNano())
	if req.Reference != "" {
		idempotencyKey = fmt.Sprintf("%s-%s", userID.String(), req.Reference)
	}

	// Check for duplicate idempotency key
	existingTransfer, err := s.transferRepo.FindByIdempotencyKey(idempotencyKey)
	if err == nil && existingTransfer != nil {
		if existingTransfer.IsPending() || existingTransfer.Status == models.TransferStatusProcessing {
			return nil, ErrTransferPending
		}
		if existingTransfer.IsFailed() {
			return nil, ErrTransferFailed
		}
	}

	// Create transfer record
	transfer := &models.Transfer{
		FromAccountID:     req.FromAccountID,
		ToAccountID:       nil, // NULL for external transfers
		Amount:            amount,
		Description:       req.Description,
		IdempotencyKey:    idempotencyKey,
		Status:            models.TransferStatusPending,
		IsExternal:        true,
		ExternalAccountID: &req.ExternalAccountID,
		TransferType:      req.TransferType,
		ScheduledAt:       scheduledAt,
	}

	// For scheduled transfers, don't initiate immediately
	if req.TransferType == models.TransferTypeScheduled {
		if err := s.transferRepo.Create(transfer); err != nil {
			return nil, fmt.Errorf("failed to create scheduled transfer: %w", err)
		}

		// Audit log
		auditLog := &models.AuditLog{
			UserID:     &userID,
			Action:     "external_transfer_scheduled",
			Resource:   "transfer",
			ResourceID: transfer.ID.String(),
			IPAddress:  ipAddress,
			UserAgent:   userAgent,
			Metadata:    models.JSONBMap{
				"amount":        amount.String(),
				"transfer_type": req.TransferType,
				"scheduled_at":  scheduledAt.Format(time.RFC3339),
			},
		}
		s.auditService.CreateAuditLog(auditLog)

		return transfer, nil
	}

	// For instant/standard transfers, initiate immediately
	return s.initiateTransferToNorthWind(transfer, fromAccount, externalAccount, currency, userID, ipAddress, userAgent)
}

// initiateTransferToNorthWind initiates the actual transfer with NorthWind
func (s *NorthWindTransferService) initiateTransferToNorthWind(
	transfer *models.Transfer,
	fromAccount *models.Account,
	externalAccount *models.ExternalAccount,
	currency string,
	userID uuid.UUID,
	ipAddress, userAgent string,
) (*models.Transfer, error) {
	// Debit from account immediately (reserve funds)
	debitTransaction := &models.Transaction{
		AccountID:       fromAccount.ID,
		TransactionType: models.TransactionTypeDebit,
		Amount:          transfer.Amount,
		BalanceBefore:   fromAccount.Balance,
		BalanceAfter:    fromAccount.Balance.Sub(transfer.Amount),
		Description:     fmt.Sprintf("Transfer to %s - %s", externalAccount.AccountName, transfer.Description),
		Status:          models.TransactionStatusPending, // Will be confirmed when NorthWind completes
		Reference:       transfer.IdempotencyKey,
	}

	if err := s.transactionRepo.Create(debitTransaction); err != nil {
		return nil, fmt.Errorf("failed to create debit transaction: %w", err)
	}

	// Update account balance (reserve funds)
	if err := s.accountRepo.UpdateBalance(fromAccount.ID, transfer.Amount.Neg(), models.TransactionTypeDebit); err != nil {
		return nil, fmt.Errorf("failed to update account balance: %w", err)
	}

	// Create transfer record
	if err := s.transferRepo.Create(transfer); err != nil {
		// Rollback: refund the account
		s.accountRepo.UpdateBalance(fromAccount.ID, transfer.Amount, models.TransactionTypeCredit)
		return nil, fmt.Errorf("failed to create transfer: %w", err)
	}

	// Link debit transaction
	transfer.DebitTransactionID = &debitTransaction.ID

	// Initiate transfer with NorthWind
	northWindReq := dto.NorthWindTransferRequest{
		FromAccountID: fromAccount.AccountNumber, // Use our account number
		ToAccountID:   externalAccount.NorthWindAccountID,
		Amount:        transfer.Amount.String(),
		Currency:      currency,
		Description:   transfer.Description,
		Reference:     transfer.IdempotencyKey,
		TransferType:  transfer.TransferType,
	}

	northWindResp, err := s.northWindService.InitiateTransfer(northWindReq)
	if err != nil {
		s.logger.Error("Failed to initiate transfer with NorthWind",
			"transfer_id", transfer.ID,
			"error", err)

		// Mark transfer as failed
		transfer.Fail(fmt.Sprintf("Failed to initiate with NorthWind: %v", err))
		transfer.DebitTransactionID = &debitTransaction.ID
		s.transferRepo.Update(transfer)

		// Refund the account
		refundTx := &models.Transaction{
			AccountID:       fromAccount.ID,
			TransactionType: models.TransactionTypeCredit,
			Amount:          transfer.Amount,
			BalanceBefore:   fromAccount.Balance.Sub(transfer.Amount),
			BalanceAfter:    fromAccount.Balance,
			Description:     fmt.Sprintf("Refund: Transfer to %s failed", externalAccount.AccountName),
			Status:          models.TransactionStatusCompleted,
			Reference:       transfer.IdempotencyKey + "-refund",
		}
		s.transactionRepo.Create(refundTx)
		s.accountRepo.UpdateBalance(fromAccount.ID, transfer.Amount, models.TransactionTypeCredit)

		// Update debit transaction status
		debitTransaction.Status = models.TransactionStatusFailed
		s.transactionRepo.UpdateStatus(debitTransaction.ID, models.TransactionStatusFailed)

		return nil, fmt.Errorf("failed to initiate transfer with NorthWind: %w", err)
	}

	// Update transfer with NorthWind response
	var estimatedCompletion *time.Time
	if !northWindResp.EstimatedCompletion.IsZero() {
		estimatedCompletion = &northWindResp.EstimatedCompletion
	}
	transfer.MarkAsProcessing(northWindResp.TransferID, estimatedCompletion)
	transfer.DebitTransactionID = &debitTransaction.ID
	if err := s.transferRepo.Update(transfer); err != nil {
		s.logger.Error("Failed to update transfer with NorthWind ID",
			"transfer_id", transfer.ID,
			"error", err)
	}

	s.logger.Info("Transfer initiated with NorthWind",
		"transfer_id", transfer.ID,
		"northwind_transfer_id", northWindResp.TransferID,
		"status", northWindResp.Status)

	// Audit log
	auditLog := &models.AuditLog{
		UserID:     &userID,
		Action:     "external_transfer_initiated",
		Resource:   "transfer",
		ResourceID: transfer.ID.String(),
		IPAddress:  ipAddress,
		UserAgent:   userAgent,
		Metadata:    models.JSONBMap{
			"amount":              transfer.Amount.String(),
			"northwind_transfer_id": northWindResp.TransferID,
			"transfer_type":       transfer.TransferType,
		},
	}
	s.auditService.CreateAuditLog(auditLog)

	return transfer, nil
}

// CheckTransferStatus checks the status of an external transfer with NorthWind
func (s *NorthWindTransferService) CheckTransferStatus(transferID uuid.UUID) (*models.Transfer, error) {
	transfer, err := s.transferRepo.FindByID(transferID)
	if err != nil {
		if errors.Is(err, repositories.ErrTransferNotFound) {
			return nil, repositories.ErrTransferNotFound
		}
		return nil, fmt.Errorf("failed to get transfer: %w", err)
	}

	if !transfer.IsExternal {
		return nil, errors.New("transfer is not an external transfer")
	}

	if transfer.NorthWindTransferID == nil || *transfer.NorthWindTransferID == "" {
		return nil, errors.New("transfer does not have a NorthWind transfer ID")
	}

	// Check status with NorthWind
	statusResp, err := s.northWindService.GetTransferStatus(*transfer.NorthWindTransferID)
	if err != nil {
		return nil, fmt.Errorf("failed to check transfer status with NorthWind: %w", err)
	}

	// Update transfer status
	now := time.Now()
	transfer.LastStatusCheck = &now
	transfer.StatusCheckCount++

	switch statusResp.Status {
	case "completed":
		if transfer.Status != models.TransferStatusCompleted {
			if err := s.completeTransfer(transfer); err != nil {
				s.logger.Error("Failed to complete transfer",
					"transfer_id", transfer.ID,
					"error", err)
			}
		}
	case "failed":
		if transfer.Status != models.TransferStatusFailed {
			errorMsg := statusResp.FailureReason
			if errorMsg == "" {
				errorMsg = "Transfer failed at NorthWind"
			}
			if err := s.failTransfer(transfer, errorMsg); err != nil {
				s.logger.Error("Failed to mark transfer as failed",
					"transfer_id", transfer.ID,
					"error", err)
			}
		}
	case "processing", "pending":
		transfer.Status = models.TransferStatusProcessing
		// EstimatedCompletion is not in the status response, keep existing if set
	}

	if err := s.transferRepo.Update(transfer); err != nil {
		return nil, fmt.Errorf("failed to update transfer status: %w", err)
	}

	return transfer, nil
}

// completeTransfer completes a transfer and finalizes the transaction
func (s *NorthWindTransferService) completeTransfer(transfer *models.Transfer) error {

	// Update debit transaction status to completed
	if transfer.DebitTransactionID != nil {
		if err := s.transactionRepo.UpdateStatus(*transfer.DebitTransactionID, models.TransactionStatusCompleted); err != nil {
			s.logger.Warn("Failed to update debit transaction status",
				"transaction_id", *transfer.DebitTransactionID,
				"error", err)
		}
	}

	// Mark transfer as completed
	now := time.Now()
	transfer.Status = models.TransferStatusCompleted
	transfer.CompletedAt = &now

	s.logger.Info("External transfer completed",
		"transfer_id", transfer.ID,
		"northwind_transfer_id", transfer.NorthWindTransferID)

	return nil
}

// failTransfer marks a transfer as failed and refunds the account
func (s *NorthWindTransferService) failTransfer(transfer *models.Transfer, errorMessage string) error {
	// Get from account
	fromAccount, err := s.accountRepo.GetByID(transfer.FromAccountID)
	if err != nil {
		return fmt.Errorf("failed to get from account: %w", err)
	}

	// Refund the account
	refundTx := &models.Transaction{
		AccountID:       fromAccount.ID,
		TransactionType: models.TransactionTypeCredit,
		Amount:          transfer.Amount,
		BalanceBefore:   fromAccount.Balance,
		BalanceAfter:    fromAccount.Balance.Add(transfer.Amount),
		Description:     fmt.Sprintf("Refund: Transfer failed - %s", errorMessage),
		Status:          models.TransactionStatusCompleted,
		Reference:       transfer.IdempotencyKey + "-refund",
	}

	if err := s.transactionRepo.Create(refundTx); err != nil {
		s.logger.Error("Failed to create refund transaction",
			"transfer_id", transfer.ID,
			"error", err)
	} else {
		// Update account balance
		if err := s.accountRepo.UpdateBalance(fromAccount.ID, transfer.Amount, models.TransactionTypeCredit); err != nil {
			s.logger.Error("Failed to refund account balance",
				"transfer_id", transfer.ID,
				"error", err)
		}
	}

	// Update debit transaction status
	if transfer.DebitTransactionID != nil {
		if err := s.transactionRepo.UpdateStatus(*transfer.DebitTransactionID, models.TransactionStatusFailed); err != nil {
			s.logger.Warn("Failed to update debit transaction status",
				"transaction_id", *transfer.DebitTransactionID,
				"error", err)
		}
	}

	// Mark transfer as failed
	transfer.Fail(errorMessage)

	s.logger.Info("External transfer failed",
		"transfer_id", transfer.ID,
		"error", errorMessage)

	return nil
}

