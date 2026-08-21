-- +goose Up
-- +goose StatementBegin
ALTER TABLE status_updates DROP COLUMN blockers;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE status_updates ADD COLUMN blockers TEXT;
-- +goose StatementEnd
