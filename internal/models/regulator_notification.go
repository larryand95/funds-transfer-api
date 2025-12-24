package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// RegulatorNotificationStatus represents the status of a webhook notification
	RegulatorNotificationStatusPending          = "pending"
	RegulatorNotificationStatusSending          = "sending"
	RegulatorNotificationStatusSent             = "sent"
	RegulatorNotificationStatusFailed           = "failed"
	RegulatorNotificationStatusRetrying         = "retrying"
	RegulatorNotificationStatusFailedPermanently = "failed_permanently"

	// RegulatorNotificationEventType represents the type of event
	RegulatorNotificationEventTypeTransferCompleted = "transfer_completed"
	RegulatorNotificationEventTypeTransferFailed    = "transfer_failed"
)

var (
	ErrInvalidNotificationStatus  = errors.New("invalid notification status")
	ErrInvalidNotificationEventType = errors.New("invalid notification event type")
	ErrNotificationDeadlinePassed  = errors.New("notification deadline has passed")
	ErrMaxRetriesExceeded          = errors.New("maximum retry attempts exceeded")
)

// RegulatorNotification represents a webhook notification sent to the regulator
type RegulatorNotification struct {
	ID uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`

	// Transfer reference
	TransferID uuid.UUID `gorm:"type:uuid;not null;index:idx_regulator_notifications_transfer_id" json:"transfer_id"`

	// Notification details
	Status    string `gorm:"type:varchar(20);not null;default:'pending';index:idx_regulator_notifications_status" json:"status"`
	EventType string `gorm:"type:varchar(20);not null" json:"event_type"`

	// Webhook details
	WebhookURL string  `gorm:"type:varchar(500);not null" json:"webhook_url"`
	Payload    JSONBMap `gorm:"type:jsonb;not null" json:"payload"`
	Headers    JSONBMap `gorm:"type:jsonb" json:"headers,omitempty"`

	// Delivery tracking
	AttemptCount int        `gorm:"default:0" json:"attempt_count"`
	MaxAttempts  int        `gorm:"default:10" json:"max_attempts"`
	NextRetryAt  *time.Time `gorm:"index:idx_regulator_notifications_next_retry" json:"next_retry_at,omitempty"`
	LastAttemptedAt *time.Time `json:"last_attempted_at,omitempty"`

	// Response tracking
	HTTPStatusCode  *int       `json:"http_status_code,omitempty"`
	ResponseBody    *string    `gorm:"type:text" json:"response_body,omitempty"`
	ResponseHeaders JSONBMap   `gorm:"type:jsonb" json:"response_headers,omitempty"`
	ErrorMessage    *string    `gorm:"type:text" json:"error_message,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `gorm:"not null" json:"created_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// Compliance
	DeadlineAt              time.Time `gorm:"not null;index:idx_regulator_notifications_deadline" json:"deadline_at"`
	NotifiedWithinDeadline  bool      `gorm:"default:false" json:"notified_within_deadline"`

	// Associations
	Transfer Transfer `gorm:"foreignKey:TransferID" json:"-"`
}

// BeforeCreate hook for RegulatorNotification
func (r *RegulatorNotification) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}

	if r.Status == "" {
		r.Status = RegulatorNotificationStatusPending
	}

	if r.MaxAttempts == 0 {
		r.MaxAttempts = 10 // Default max attempts
	}

	now := time.Now()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}

	// Set deadline to 60 seconds from creation if not set
	if r.DeadlineAt.IsZero() {
		r.DeadlineAt = now.Add(60 * time.Second)
	}

	return r.Validate()
}

// BeforeUpdate hook for RegulatorNotification
func (r *RegulatorNotification) BeforeUpdate(tx *gorm.DB) error {
	return r.Validate()
}

// Validate validates the notification fields
func (r *RegulatorNotification) Validate() error {
	if r.TransferID == uuid.Nil {
		return errors.New("transfer ID is required")
	}

	if r.WebhookURL == "" {
		return errors.New("webhook URL is required")
	}

	if r.Payload == nil || len(r.Payload) == 0 {
		return errors.New("payload is required")
	}

	if !IsValidNotificationStatus(r.Status) {
		return ErrInvalidNotificationStatus
	}

	if !IsValidNotificationEventType(r.EventType) {
		return ErrInvalidNotificationEventType
	}

	if r.AttemptCount < 0 {
		return errors.New("attempt count cannot be negative")
	}

	if r.AttemptCount > r.MaxAttempts {
		return ErrMaxRetriesExceeded
	}

	return nil
}

// IsPending returns true if the notification is pending
func (r *RegulatorNotification) IsPending() bool {
	return r.Status == RegulatorNotificationStatusPending
}

// IsSent returns true if the notification was successfully sent
func (r *RegulatorNotification) IsSent() bool {
	return r.Status == RegulatorNotificationStatusSent
}

// IsFailed returns true if the notification failed
func (r *RegulatorNotification) IsFailed() bool {
	return r.Status == RegulatorNotificationStatusFailed || r.Status == RegulatorNotificationStatusFailedPermanently
}

// CanRetry returns true if the notification can be retried
func (r *RegulatorNotification) CanRetry() bool {
	return r.Status == RegulatorNotificationStatusFailed || r.Status == RegulatorNotificationStatusRetrying
}

// NeedsRetry returns true if the notification needs to be retried
func (r *RegulatorNotification) NeedsRetry() bool {
	if !r.CanRetry() {
		return false
	}

	if r.AttemptCount >= r.MaxAttempts {
		return false
	}

	if r.NextRetryAt == nil {
		return true
	}

	return time.Now().After(*r.NextRetryAt)
}

// MarkAsSending marks the notification as currently being sent
func (r *RegulatorNotification) MarkAsSending() {
	r.Status = RegulatorNotificationStatusSending
	now := time.Now()
	r.LastAttemptedAt = &now
	r.AttemptCount++
}

// MarkAsSent marks the notification as successfully sent
func (r *RegulatorNotification) MarkAsSent(httpStatusCode int, responseBody *string, responseHeaders JSONBMap) {
	r.Status = RegulatorNotificationStatusSent
	now := time.Now()
	r.SentAt = &now
	r.CompletedAt = &now
	r.HTTPStatusCode = &httpStatusCode
	r.ResponseBody = responseBody
	r.ResponseHeaders = responseHeaders

	// Check if sent within deadline
	if now.Before(r.DeadlineAt) {
		r.NotifiedWithinDeadline = true
	} else {
		r.NotifiedWithinDeadline = false
	}
}

// MarkAsFailed marks the notification as failed and schedules retry
func (r *RegulatorNotification) MarkAsFailed(errorMessage string, httpStatusCode *int, responseBody *string) {
	r.Status = RegulatorNotificationStatusFailed
	r.ErrorMessage = &errorMessage
	r.HTTPStatusCode = httpStatusCode
	if responseBody != nil {
		r.ResponseBody = responseBody
	}

	// Schedule next retry if attempts remaining
	if r.AttemptCount < r.MaxAttempts {
		r.Status = RegulatorNotificationStatusRetrying
		r.NextRetryAt = r.calculateNextRetry()
	} else {
		r.Status = RegulatorNotificationStatusFailedPermanently
		now := time.Now()
		r.CompletedAt = &now
	}
}

// calculateNextRetry calculates the next retry time using exponential backoff
func (r *RegulatorNotification) calculateNextRetry() *time.Time {
	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s, 60s (max)
	backoffIntervals := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second, // Max delay
	}

	attemptIndex := r.AttemptCount - 1
	if attemptIndex >= len(backoffIntervals) {
		attemptIndex = len(backoffIntervals) - 1
	}

	delay := backoffIntervals[attemptIndex]
	nextRetry := time.Now().Add(delay)
	return &nextRetry
}

// IsDeadlineViolated returns true if the deadline has passed
func (r *RegulatorNotification) IsDeadlineViolated() bool {
	return time.Now().After(r.DeadlineAt)
}

// TableName returns the table name for RegulatorNotification
func (r *RegulatorNotification) TableName() string {
	return "regulator_notifications"
}

// Helper functions

// IsValidNotificationStatus checks if the status is valid
func IsValidNotificationStatus(status string) bool {
	switch status {
	case RegulatorNotificationStatusPending,
		RegulatorNotificationStatusSending,
		RegulatorNotificationStatusSent,
		RegulatorNotificationStatusFailed,
		RegulatorNotificationStatusRetrying,
		RegulatorNotificationStatusFailedPermanently:
		return true
	default:
		return false
	}
}

// IsValidNotificationEventType checks if the event type is valid
func IsValidNotificationEventType(eventType string) bool {
	switch eventType {
	case RegulatorNotificationEventTypeTransferCompleted,
		RegulatorNotificationEventTypeTransferFailed:
		return true
	default:
		return false
	}
}

