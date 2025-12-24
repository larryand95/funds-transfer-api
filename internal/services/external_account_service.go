package services

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/array/banking-api/internal/dto"
	"github.com/array/banking-api/internal/models"
	"github.com/array/banking-api/internal/repositories"
	"github.com/google/uuid"
)

var (
	ErrExternalAccountNotFound      = errors.New("external account not found")
	ErrExternalAccountAlreadyExists = errors.New("external account already exists for this user")
	ErrExternalAccountNotVerified   = errors.New("external account is not verified")
	ErrExternalAccountVerificationFailed = errors.New("external account verification failed")
)

// ExternalAccountService handles external account management
type ExternalAccountService struct {
	externalAccountRepo repositories.ExternalAccountRepositoryInterface
	userRepo            repositories.UserRepositoryInterface
	northWindService    NorthWindServiceInterface
	auditService        AuditServiceInterface
	logger              *slog.Logger
}

// NewExternalAccountService creates a new external account service
func NewExternalAccountService(
	externalAccountRepo repositories.ExternalAccountRepositoryInterface,
	userRepo repositories.UserRepositoryInterface,
	northWindService NorthWindServiceInterface,
	auditService AuditServiceInterface,
	logger *slog.Logger,
) *ExternalAccountService {
	if logger == nil {
		logger = slog.Default()
	}

	return &ExternalAccountService{
		externalAccountRepo: externalAccountRepo,
		userRepo:            userRepo,
		northWindService:    northWindService,
		auditService:        auditService,
		logger:              logger,
	}
}

// RegisterExternalAccount registers a new external NorthWind account for a user
func (s *ExternalAccountService) RegisterExternalAccount(userID uuid.UUID, req dto.RegisterExternalAccountRequest, ipAddress, userAgent string) (*models.ExternalAccount, error) {
	// Verify user exists
	_, err := s.userRepo.GetByID(userID)
	if err != nil {
		if errors.Is(err, repositories.ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to verify user: %w", err)
	}

	// Check if account already exists for this user
	exists, err := s.externalAccountRepo.ExistsForUser(userID, req.AccountNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing account: %w", err)
	}
	if exists {
		return nil, ErrExternalAccountAlreadyExists
	}

	// Set defaults
	currency := req.Currency
	if currency == "" {
		currency = "USD"
	}

	// Create external account
	externalAccount := &models.ExternalAccount{
		UserID:        userID,
		AccountNumber: req.AccountNumber,
		AccountName:   req.AccountName,
		BankName:      "NorthWind Bank",
		BankCode:      req.BankCode,
		Currency:      currency,
		Status:        models.ExternalAccountStatusPending,
		Verified:      false,
		Nickname:      req.Nickname,
	}

	// Save to database first
	if err := s.externalAccountRepo.Create(externalAccount); err != nil {
		return nil, fmt.Errorf("failed to create external account: %w", err)
	}

	// Verify account with NorthWind in background (async)
	go s.verifyAccountWithNorthWind(externalAccount.ID, req)

	// Audit log
	auditLog := &models.AuditLog{
		UserID:     &userID,
		Action:     "external_account_registered",
		Resource:   "external_account",
		ResourceID: externalAccount.ID.String(),
		IPAddress:  ipAddress,
		UserAgent:   userAgent,
		Metadata:    models.JSONBMap{"account_number": req.AccountNumber, "account_name": req.AccountName},
	}
	s.auditService.CreateAuditLog(auditLog)

	return externalAccount, nil
}

// verifyAccountWithNorthWind verifies an external account with NorthWind API
func (s *ExternalAccountService) verifyAccountWithNorthWind(externalAccountID uuid.UUID, req dto.RegisterExternalAccountRequest) {
	externalAccount, err := s.externalAccountRepo.FindByID(externalAccountID)
	if err != nil {
		s.logger.Error("Failed to find external account for verification",
			"external_account_id", externalAccountID,
			"error", err)
		return
	}

	// Verify with NorthWind
	verifyReq := dto.NorthWindAccountVerificationRequest{
		AccountNumber: req.AccountNumber,
		AccountName:   req.AccountName,
		BankCode:      req.BankCode,
	}

	verifyResp, err := s.northWindService.VerifyAccount(verifyReq)
	if err != nil {
		s.logger.Error("Failed to verify account with NorthWind",
			"external_account_id", externalAccountID,
			"error", err)

		externalAccount.MarkAsFailed(fmt.Sprintf("Verification failed: %v", err))
		if updateErr := s.externalAccountRepo.Update(externalAccount); updateErr != nil {
			s.logger.Error("Failed to update external account status",
				"external_account_id", externalAccountID,
				"error", updateErr)
		}
		return
	}

	// Update account based on verification result
	if verifyResp.IsValid && verifyResp.IsVerified {
		externalAccount.MarkAsVerified(verifyResp.AccountID)
		s.logger.Info("External account verified successfully",
			"external_account_id", externalAccountID,
			"northwind_account_id", verifyResp.AccountID)
	} else {
		errorMsg := verifyResp.Message
		if errorMsg == "" {
			errorMsg = "Account verification failed"
		}
		externalAccount.MarkAsFailed(errorMsg)
		s.logger.Warn("External account verification failed",
			"external_account_id", externalAccountID,
			"message", errorMsg)
	}

	if err := s.externalAccountRepo.Update(externalAccount); err != nil {
		s.logger.Error("Failed to update external account after verification",
			"external_account_id", externalAccountID,
			"error", err)
	}
}

// GetExternalAccounts retrieves all external accounts for a user
func (s *ExternalAccountService) GetExternalAccounts(userID uuid.UUID) ([]models.ExternalAccount, error) {
	accounts, err := s.externalAccountRepo.FindByUserID(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get external accounts: %w", err)
	}

	return accounts, nil
}

// GetExternalAccount retrieves a specific external account
func (s *ExternalAccountService) GetExternalAccount(userID, externalAccountID uuid.UUID) (*models.ExternalAccount, error) {
	account, err := s.externalAccountRepo.FindByID(externalAccountID)
	if err != nil {
		if errors.Is(err, repositories.ErrExternalAccountNotFound) {
			return nil, ErrExternalAccountNotFound
		}
		return nil, fmt.Errorf("failed to get external account: %w", err)
	}

	// Verify ownership
	if account.UserID != userID {
		return nil, ErrUnauthorized
	}

	return account, nil
}

// DeleteExternalAccount deletes an external account
func (s *ExternalAccountService) DeleteExternalAccount(userID, externalAccountID uuid.UUID, ipAddress, userAgent string) error {
	account, err := s.externalAccountRepo.FindByID(externalAccountID)
	if err != nil {
		if errors.Is(err, repositories.ErrExternalAccountNotFound) {
			return ErrExternalAccountNotFound
		}
		return fmt.Errorf("failed to get external account: %w", err)
	}

	// Verify ownership
	if account.UserID != userID {
		return ErrUnauthorized
	}

	if err := s.externalAccountRepo.Delete(externalAccountID); err != nil {
		return fmt.Errorf("failed to delete external account: %w", err)
	}

	// Audit log
	auditLog := &models.AuditLog{
		UserID:     &userID,
		Action:     "external_account_deleted",
		Resource:   "external_account",
		ResourceID: externalAccountID.String(),
		IPAddress:  ipAddress,
		UserAgent:   userAgent,
		Metadata:    models.JSONBMap{"account_number": account.AccountNumber},
	}
	s.auditService.CreateAuditLog(auditLog)

	return nil
}

