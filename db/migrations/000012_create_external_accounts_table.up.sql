-- ============================================================================
-- PostgreSQL Database Schema
-- External Accounts Table
-- ============================================================================
-- 
-- This file contains the complete SQL schema for:
-- external_accounts - NorthWind Bank external accounts registered by users
--
-- ============================================================================

-- ============================================================================
-- EXTERNAL ACCOUNTS TABLE
-- ============================================================================
-- Stores NorthWind Bank accounts registered by customers for external transfers

CREATE TABLE IF NOT EXISTS external_accounts (
    -- Primary Key
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    
    -- Foreign Key to users table
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    
    -- Account Information
    account_number VARCHAR(50) NOT NULL,
    account_name VARCHAR(255) NOT NULL,
    bank_name VARCHAR(100) NOT NULL DEFAULT 'NorthWind Bank',
    bank_code VARCHAR(20),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    
    -- NorthWind Integration
    northwind_account_id VARCHAR(100),
    
    -- Verification Status
    status VARCHAR(20) NOT NULL DEFAULT 'pending' 
        CHECK (status IN ('pending', 'verified', 'failed', 'inactive')),
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    verification_error TEXT,
    verified_at TIMESTAMP,
    
    -- User-friendly name
    nickname VARCHAR(100),
    
    -- Timestamps
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP NULL
);

-- Indexes for external_accounts table
CREATE INDEX IF NOT EXISTS idx_external_accounts_user_id ON external_accounts(user_id);
CREATE INDEX IF NOT EXISTS idx_external_accounts_northwind_account_id ON external_accounts(northwind_account_id) 
    WHERE northwind_account_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_external_accounts_status ON external_accounts(status);
CREATE INDEX IF NOT EXISTS idx_external_accounts_deleted_at ON external_accounts(deleted_at) 
    WHERE deleted_at IS NOT NULL;

-- Unique constraint: One account number per user
CREATE UNIQUE INDEX IF NOT EXISTS idx_external_accounts_user_account_number 
    ON external_accounts(user_id, account_number) 
    WHERE deleted_at IS NULL;

-- Trigger to update updated_at automatically
CREATE OR REPLACE FUNCTION update_external_accounts_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_external_accounts_updated_at
    BEFORE UPDATE ON external_accounts
    FOR EACH ROW
    EXECUTE FUNCTION update_external_accounts_updated_at();

-- Add table and column comments
COMMENT ON TABLE external_accounts IS 'NorthWind Bank external accounts registered by customers for transfers';
COMMENT ON COLUMN external_accounts.id IS 'Primary key UUID';
COMMENT ON COLUMN external_accounts.user_id IS 'Foreign key to users table';
COMMENT ON COLUMN external_accounts.account_number IS 'NorthWind Bank account number';
COMMENT ON COLUMN external_accounts.account_name IS 'Account holder name';
COMMENT ON COLUMN external_accounts.bank_name IS 'Bank name (default: NorthWind Bank)';
COMMENT ON COLUMN external_accounts.bank_code IS 'Bank code/identifier';
COMMENT ON COLUMN external_accounts.currency IS 'Account currency (default: USD)';
COMMENT ON COLUMN external_accounts.northwind_account_id IS 'Account ID returned from NorthWind API after verification';
COMMENT ON COLUMN external_accounts.status IS 'Verification status: pending, verified, failed, inactive';
COMMENT ON COLUMN external_accounts.verified IS 'Whether the account has been verified';
COMMENT ON COLUMN external_accounts.verification_error IS 'Error message if verification failed';
COMMENT ON COLUMN external_accounts.verified_at IS 'Timestamp when account was verified';
COMMENT ON COLUMN external_accounts.nickname IS 'User-friendly name for the account';
COMMENT ON COLUMN external_accounts.deleted_at IS 'Soft delete timestamp (NULL if not deleted)';

-- ============================================================================
-- SAMPLE QUERIES
-- ============================================================================

-- Get all verified external accounts for a user
-- SELECT * FROM external_accounts 
-- WHERE user_id = 'user-uuid' 
--   AND status = 'verified' 
--   AND deleted_at IS NULL
-- ORDER BY created_at DESC;

-- Get all pending verification accounts
-- SELECT * FROM external_accounts 
-- WHERE status = 'pending' 
--   AND deleted_at IS NULL
-- ORDER BY created_at ASC;

-- Get external account by NorthWind account ID
-- SELECT * FROM external_accounts 
-- WHERE northwind_account_id = 'northwind-account-id'
--   AND deleted_at IS NULL;

-- Count external accounts by status for a user
-- SELECT status, COUNT(*) as count
-- FROM external_accounts
-- WHERE user_id = 'user-uuid'
--   AND deleted_at IS NULL
-- GROUP BY status;

-- ============================================================================
-- NOTES
-- ============================================================================
-- 
-- 1. Uses soft deletes (deleted_at column) to preserve audit trail
-- 2. Unique constraint ensures one account number per user (excluding deleted records)
-- 3. Status values: pending, verified, failed, inactive
-- 4. Verification happens asynchronously via NorthWind API
-- 5. Uses UUID primary key for better distribution and security
-- 6. Indexes are optimized for common query patterns:
--    - Querying by user_id
--    - Filtering by status
--    - Looking up by NorthWind account ID
--    - Soft delete queries
-- 7. Trigger automatically updates updated_at on row modification
-- 8. Foreign key constraint ensures referential integrity with users table
--
-- ============================================================================

