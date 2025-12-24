-- ============================================================================
-- PostgreSQL Database Schema
-- Regulator Notifications Table
-- ============================================================================
-- 
-- This file contains the complete SQL schema for:
-- regulator_notifications - Webhook notifications sent to regulator
--
-- ============================================================================

-- ============================================================================
-- REGULATOR NOTIFICATIONS TABLE
-- ============================================================================
-- Stores webhook notifications sent to regulator for compliance tracking

CREATE TABLE IF NOT EXISTS regulator_notifications (
    -- Primary Key
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    
    -- Foreign Key to transfers table
    transfer_id UUID NOT NULL REFERENCES transfers(id) ON DELETE CASCADE,
    
    -- Notification Details
    status VARCHAR(20) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'retrying', 'failed_permanently')),
    event_type VARCHAR(20) NOT NULL 
        CHECK (event_type IN ('transfer_completed', 'transfer_failed')),
    
    -- Webhook Details
    webhook_url VARCHAR(500) NOT NULL,
    payload JSONB NOT NULL,
    headers JSONB,
    
    -- Delivery Tracking
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 10,
    next_retry_at TIMESTAMP,
    last_attempted_at TIMESTAMP,
    
    -- Response Tracking
    http_status_code INTEGER,
    response_body TEXT,
    response_headers JSONB,
    error_message TEXT,
    
    -- Timestamps
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at TIMESTAMP,
    completed_at TIMESTAMP,
    
    -- Compliance
    deadline_at TIMESTAMP NOT NULL,
    notified_within_deadline BOOLEAN NOT NULL DEFAULT FALSE
);

-- Indexes for regulator_notifications table
CREATE INDEX IF NOT EXISTS idx_regulator_notifications_transfer_id 
    ON regulator_notifications(transfer_id);
CREATE INDEX IF NOT EXISTS idx_regulator_notifications_status 
    ON regulator_notifications(status);
CREATE INDEX IF NOT EXISTS idx_regulator_notifications_next_retry 
    ON regulator_notifications(next_retry_at) 
    WHERE status IN ('failed', 'retrying');
CREATE INDEX IF NOT EXISTS idx_regulator_notifications_deadline 
    ON regulator_notifications(deadline_at) 
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_regulator_notifications_event_type 
    ON regulator_notifications(event_type);
CREATE INDEX IF NOT EXISTS idx_regulator_notifications_created_at 
    ON regulator_notifications(created_at);

-- Add table and column comments
COMMENT ON TABLE regulator_notifications IS 'Webhook notifications sent to regulator for transfer events (compliance tracking)';
COMMENT ON COLUMN regulator_notifications.id IS 'Primary key UUID';
COMMENT ON COLUMN regulator_notifications.transfer_id IS 'Foreign key to transfers table';
COMMENT ON COLUMN regulator_notifications.status IS 'Notification status: pending, sending, sent, failed, retrying, failed_permanently';
COMMENT ON COLUMN regulator_notifications.event_type IS 'Event type: transfer_completed, transfer_failed';
COMMENT ON COLUMN regulator_notifications.webhook_url IS 'Regulator webhook URL to send notification to';
COMMENT ON COLUMN regulator_notifications.payload IS 'JSON payload sent to regulator';
COMMENT ON COLUMN regulator_notifications.headers IS 'HTTP headers sent with webhook (e.g., HMAC signature)';
COMMENT ON COLUMN regulator_notifications.attempt_count IS 'Number of delivery attempts made';
COMMENT ON COLUMN regulator_notifications.max_attempts IS 'Maximum number of retry attempts (default: 10)';
COMMENT ON COLUMN regulator_notifications.next_retry_at IS 'Next scheduled retry time (exponential backoff)';
COMMENT ON COLUMN regulator_notifications.last_attempted_at IS 'Timestamp of last delivery attempt';
COMMENT ON COLUMN regulator_notifications.http_status_code IS 'HTTP status code from regulator response';
COMMENT ON COLUMN regulator_notifications.response_body IS 'Response body from regulator webhook';
COMMENT ON COLUMN regulator_notifications.response_headers IS 'Response headers from regulator webhook';
COMMENT ON COLUMN regulator_notifications.error_message IS 'Error message if delivery failed';
COMMENT ON COLUMN regulator_notifications.deadline_at IS 'Deadline for notification (60 seconds from transfer event)';
COMMENT ON COLUMN regulator_notifications.notified_within_deadline IS 'Whether notification was sent within the 60-second deadline';

-- ============================================================================
-- SAMPLE QUERIES
-- ============================================================================

-- Get pending notifications that need to be sent
-- SELECT * FROM regulator_notifications 
-- WHERE status = 'pending' 
--   AND deadline_at > CURRENT_TIMESTAMP
-- ORDER BY created_at ASC
-- LIMIT 100;

-- Get notifications that need retry
-- SELECT * FROM regulator_notifications 
-- WHERE status IN ('failed', 'retrying')
--   AND attempt_count < max_attempts
--   AND (next_retry_at IS NULL OR next_retry_at <= CURRENT_TIMESTAMP)
-- ORDER BY next_retry_at ASC
-- LIMIT 100;

-- Get all notifications for a specific transfer
-- SELECT * FROM regulator_notifications 
-- WHERE transfer_id = 'transfer-uuid'
-- ORDER BY created_at DESC;

-- Get compliance statistics (notifications sent within deadline)
-- SELECT 
--     COUNT(*) as total_notifications,
--     COUNT(*) FILTER (WHERE notified_within_deadline = TRUE) as within_deadline,
--     COUNT(*) FILTER (WHERE notified_within_deadline = FALSE) as after_deadline,
--     COUNT(*) FILTER (WHERE status = 'sent') as successfully_sent,
--     COUNT(*) FILTER (WHERE status = 'failed_permanently') as permanently_failed
-- FROM regulator_notifications
-- WHERE created_at >= CURRENT_DATE - INTERVAL '30 days';

-- Get notifications approaching deadline
-- SELECT * FROM regulator_notifications 
-- WHERE status = 'pending'
--   AND deadline_at BETWEEN CURRENT_TIMESTAMP AND CURRENT_TIMESTAMP + INTERVAL '10 seconds'
-- ORDER BY deadline_at ASC;

-- Get failed notifications by event type
-- SELECT 
--     event_type,
--     COUNT(*) as failed_count,
--     AVG(attempt_count) as avg_attempts
-- FROM regulator_notifications
-- WHERE status IN ('failed', 'failed_permanently')
-- GROUP BY event_type;

-- ============================================================================
-- NOTES
-- ============================================================================
-- 
-- 1. Tracks webhook delivery to regulator for compliance purposes
-- 2. Implements exponential backoff for retries (1s, 2s, 4s, 8s, 16s, 32s, 60s max)
-- 3. Monitors 60-second deadline for compliance tracking
-- 4. Stores full request/response for audit trail
-- 5. Status values: pending, sending, sent, failed, retrying, failed_permanently
-- 6. Event types: transfer_completed, transfer_failed
-- 7. Uses UUID primary key for better distribution and security
-- 8. Indexes are optimized for common query patterns:
--    - Querying by transfer_id
--    - Finding pending notifications
--    - Finding notifications that need retry
--    - Finding notifications approaching deadline
--    - Time-based queries
-- 9. Foreign key constraint ensures referential integrity with transfers table
-- 10. JSONB columns (payload, headers, response_headers) allow flexible storage
--     and efficient querying of JSON data
--
-- ============================================================================

