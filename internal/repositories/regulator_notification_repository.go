package repositories

import (
	"errors"
	"fmt"
	"time"

	"github.com/array/banking-api/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrRegulatorNotificationNotFound = errors.New("regulator notification not found")
)

// regulatorNotificationRepository implements RegulatorNotificationRepositoryInterface
type regulatorNotificationRepository struct {
	db *gorm.DB
}

// NewRegulatorNotificationRepository creates a new regulator notification repository
func NewRegulatorNotificationRepository(db *gorm.DB) RegulatorNotificationRepositoryInterface {
	return &regulatorNotificationRepository{
		db: db,
	}
}

// Create creates a new regulator notification
func (r *regulatorNotificationRepository) Create(notification *models.RegulatorNotification) error {
	if notification == nil {
		return errors.New("notification cannot be nil")
	}

	if err := r.db.Create(notification).Error; err != nil {
		return fmt.Errorf("failed to create regulator notification: %w", err)
	}

	return nil
}

// Update updates an existing regulator notification
func (r *regulatorNotificationRepository) Update(notification *models.RegulatorNotification) error {
	if notification == nil {
		return errors.New("notification cannot be nil")
	}

	if err := r.db.Save(notification).Error; err != nil {
		return fmt.Errorf("failed to update regulator notification: %w", err)
	}

	return nil
}

// FindByID retrieves a regulator notification by ID
func (r *regulatorNotificationRepository) FindByID(id uuid.UUID) (*models.RegulatorNotification, error) {
	var notification models.RegulatorNotification
	if err := r.db.Where("id = ?", id).First(&notification).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRegulatorNotificationNotFound
		}
		return nil, fmt.Errorf("failed to find regulator notification by ID: %w", err)
	}

	return &notification, nil
}

// FindByTransferID retrieves all notifications for a specific transfer
func (r *regulatorNotificationRepository) FindByTransferID(transferID uuid.UUID) ([]models.RegulatorNotification, error) {
	var notifications []models.RegulatorNotification
	if err := r.db.Where("transfer_id = ?", transferID).
		Order("created_at DESC").
		Find(&notifications).Error; err != nil {
		return nil, fmt.Errorf("failed to find notifications by transfer ID: %w", err)
	}

	return notifications, nil
}

// FindPendingNotifications retrieves pending notifications that need to be sent
func (r *regulatorNotificationRepository) FindPendingNotifications(limit int) ([]models.RegulatorNotification, error) {
	var notifications []models.RegulatorNotification
	if err := r.db.Where("status = ?", models.RegulatorNotificationStatusPending).
		Where("deadline_at > ?", time.Now()).
		Order("deadline_at ASC").
		Limit(limit).
		Find(&notifications).Error; err != nil {
		return nil, fmt.Errorf("failed to find pending notifications: %w", err)
	}

	return notifications, nil
}

// FindRetryableNotifications retrieves notifications that need to be retried
func (r *regulatorNotificationRepository) FindRetryableNotifications(limit int) ([]models.RegulatorNotification, error) {
	var notifications []models.RegulatorNotification
	now := time.Now()
	
	if err := r.db.Where("status IN ?", []string{
		models.RegulatorNotificationStatusFailed,
		models.RegulatorNotificationStatusRetrying,
	}).
		Where("next_retry_at IS NOT NULL AND next_retry_at <= ?", now).
		Where("attempt_count < max_attempts").
		Order("next_retry_at ASC").
		Limit(limit).
		Find(&notifications).Error; err != nil {
		return nil, fmt.Errorf("failed to find retryable notifications: %w", err)
	}

	return notifications, nil
}

// FindByStatus retrieves notifications by status
func (r *regulatorNotificationRepository) FindByStatus(status string, limit int) ([]models.RegulatorNotification, error) {
	var notifications []models.RegulatorNotification
	if err := r.db.Where("status = ?", status).
		Order("created_at DESC").
		Limit(limit).
		Find(&notifications).Error; err != nil {
		return nil, fmt.Errorf("failed to find notifications by status: %w", err)
	}

	return notifications, nil
}

// CountByStatus counts notifications by status
func (r *regulatorNotificationRepository) CountByStatus(status string) (int64, error) {
	var count int64
	if err := r.db.Model(&models.RegulatorNotification{}).
		Where("status = ?", status).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count notifications by status: %w", err)
	}

	return count, nil
}

// FindApproachingDeadline retrieves notifications approaching their deadline
func (r *regulatorNotificationRepository) FindApproachingDeadline(deadlineThreshold time.Duration, limit int) ([]models.RegulatorNotification, error) {
	var notifications []models.RegulatorNotification
	threshold := time.Now().Add(deadlineThreshold)
	
	if err := r.db.Where("status = ?", models.RegulatorNotificationStatusPending).
		Where("deadline_at <= ?", threshold).
		Where("deadline_at > ?", time.Now()).
		Order("deadline_at ASC").
		Limit(limit).
		Find(&notifications).Error; err != nil {
		return nil, fmt.Errorf("failed to find notifications approaching deadline: %w", err)
	}

	return notifications, nil
}

