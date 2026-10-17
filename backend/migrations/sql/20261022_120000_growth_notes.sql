-- +goose Up
-- +goose StatementBegin
-- Notes the customer keeps on a source or a lead, and whether a lead was added by hand.
ALTER TABLE growth_sources ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT '';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'ai';
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_origin_check;
ALTER TABLE leads ADD CONSTRAINT leads_origin_check CHECK (origin IN ('ai', 'manual'));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_origin_check;
ALTER TABLE leads DROP COLUMN IF EXISTS origin;
ALTER TABLE leads DROP COLUMN IF EXISTS note;
ALTER TABLE growth_sources DROP COLUMN IF EXISTS note;
-- +goose StatementEnd
