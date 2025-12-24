package repositories

import (
	"errors"
	"fmt"

	"github.com/array/banking-api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrExternalAccountNotFound = errors.New("external account not found")
)

// externalAccountRepository implements ExternalAccountRepositoryInterface
type externalAccountRepository struct {
	db *gorm.DB
}

// NewExternalAccountRepository creates a new external account repository
func NewExternalAccountRepository(db *gorm.DB) ExternalAccountRepositoryInterface {
	return &externalAccountRepository{
		db: db,
	}
}

// Create creates a new external account
func (r *externalAccountRepository) Create(externalAccount *models.ExternalAccount) error {
	if externalAccount == nil {
		return errors.New("external account cannot be nil")
	}

	if err := r.db.Create(externalAccount).Error; err != nil {
		return fmt.Errorf("failed to create external account: %w", err)
	}

	return nil
}

// Update updates an existing external account
func (r *externalAccountRepository) Update(externalAccount *models.ExternalAccount) error {
	if externalAccount == nil {
		return errors.New("external account cannot be nil")
	}

	if err := r.db.Save(externalAccount).Error; err != nil {
		return fmt.Errorf("failed to update external account: %w", err)
	}

	return nil
}

// FindByID retrieves an external account by ID
func (r *externalAccountRepository) FindByID(id uuid.UUID) (*models.ExternalAccount, error) {
	var externalAccount models.ExternalAccount
	if err := r.db.Where("id = ?", id).First(&externalAccount).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrExternalAccountNotFound
		}
		return nil, fmt.Errorf("failed to find external account by ID: %w", err)
	}

	return &externalAccount, nil
}

// FindByUserID retrieves all external accounts for a user
func (r *externalAccountRepository) FindByUserID(userID uuid.UUID) ([]models.ExternalAccount, error) {
	var externalAccounts []models.ExternalAccount
	if err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&externalAccounts).Error; err != nil {
		return nil, fmt.Errorf("failed to find external accounts by user ID: %w", err)
	}

	return externalAccounts, nil
}

// FindByUserIDAndAccountNumber retrieves an external account by user ID and account number
func (r *externalAccountRepository) FindByUserIDAndAccountNumber(userID uuid.UUID, accountNumber string) (*models.ExternalAccount, error) {
	var externalAccount models.ExternalAccount
	if err := r.db.Where("user_id = ? AND account_number = ?", userID, accountNumber).First(&externalAccount).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrExternalAccountNotFound
		}
		return nil, fmt.Errorf("failed to find external account: %w", err)
	}

	return &externalAccount, nil
}

// Delete soft deletes an external account
func (r *externalAccountRepository) Delete(id uuid.UUID) error {
	if err := r.db.Delete(&models.ExternalAccount{}, id).Error; err != nil {
		return fmt.Errorf("failed to delete external account: %w", err)
	}

	return nil
}

// ExistsForUser checks if an external account exists for a user with the given account number
func (r *externalAccountRepository) ExistsForUser(userID uuid.UUID, accountNumber string) (bool, error) {
	var count int64
	if err := r.db.Model(&models.ExternalAccount{}).
		Where("user_id = ? AND account_number = ?", userID, accountNumber).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check external account existence: %w", err)
	}

	return count > 0, nil
}
