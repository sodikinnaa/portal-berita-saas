package postgresstore

import (
	"context"
	"database/sql"
	"fmt"

	"porta-berita/internal/cms"
)

func (s *PostgresStore) GetUserByEmail(ctx context.Context, email string) (*cms.User, error) {
	return s.userByEmail(ctx, email)
}

func (s *PostgresStore) InsertEmail(ctx context.Context, email *cms.Email) error {
	var metadataJSON []byte
	if email.Metadata != "" {
		metadataJSON = []byte(email.Metadata)
	} else {
		metadataJSON = []byte("{}")
	}

	query := `
		INSERT INTO emails (
			id, user_id, direction, sender, sender_name, recipient, 
			subject, body_html, body_text, status, error_message, 
			created_at, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err := s.db.ExecContext(ctx, query,
		email.ID,
		email.UserID,
		email.Direction,
		email.Sender,
		email.SenderName,
		email.Recipient,
		email.Subject,
		email.BodyHTML,
		email.BodyText,
		email.Status,
		email.ErrorMessage,
		email.CreatedAt,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to insert email: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetEmailByID(ctx context.Context, id string) (*cms.Email, error) {
	query := `
		SELECT id, user_id, direction, sender, sender_name, recipient, 
		       subject, body_html, body_text, status, error_message, 
		       created_at, metadata
		FROM emails
		WHERE id = $1
	`
	row := s.db.QueryRowContext(ctx, query, id)
	return scanEmail(row)
}

func (s *PostgresStore) ListEmails(ctx context.Context, userID string, direction string, limit, offset int) ([]cms.Email, error) {
	var query string
	var rows *sql.Rows
	var err error

	if userID == "" {
		if direction != "" {
			query = `
				SELECT id, user_id, direction, sender, sender_name, recipient, 
				       subject, body_html, body_text, status, error_message, 
				       created_at, metadata
				FROM emails
				WHERE direction = $1
				ORDER BY created_at DESC
				LIMIT $2 OFFSET $3
			`
			rows, err = s.db.QueryContext(ctx, query, direction, limit, offset)
		} else {
			query = `
				SELECT id, user_id, direction, sender, sender_name, recipient, 
				       subject, body_html, body_text, status, error_message, 
				       created_at, metadata
				FROM emails
				ORDER BY created_at DESC
				LIMIT $1 OFFSET $2
			`
			rows, err = s.db.QueryContext(ctx, query, limit, offset)
		}
	} else {
		if direction != "" {
			query = `
				SELECT id, user_id, direction, sender, sender_name, recipient, 
				       subject, body_html, body_text, status, error_message, 
				       created_at, metadata
				FROM emails
				WHERE user_id = $1 AND direction = $2
				ORDER BY created_at DESC
				LIMIT $3 OFFSET $4
			`
			rows, err = s.db.QueryContext(ctx, query, userID, direction, limit, offset)
		} else {
			query = `
				SELECT id, user_id, direction, sender, sender_name, recipient, 
				       subject, body_html, body_text, status, error_message, 
				       created_at, metadata
				FROM emails
				WHERE user_id = $1
				ORDER BY created_at DESC
				LIMIT $2 OFFSET $3
			`
			rows, err = s.db.QueryContext(ctx, query, userID, limit, offset)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list emails: %w", err)
	}
	defer rows.Close()

	var emails []cms.Email
	for rows.Next() {
		email, err := scanEmail(rows)
		if err != nil {
			return nil, err
		}
		emails = append(emails, *email)
	}
	return emails, nil
}

func (s *PostgresStore) MarkEmailAsRead(ctx context.Context, id string) error {
	query := `UPDATE emails SET status = 'read' WHERE id = $1`
	_, err := s.db.ExecContext(ctx, query, id)
	return err
}

func scanEmail(row scanner) (*cms.Email, error) {
	var email cms.Email
	var metadataBytes []byte
	var userID sql.NullString
	var senderName sql.NullString
	var errMsg sql.NullString

	err := row.Scan(
		&email.ID,
		&userID,
		&email.Direction,
		&email.Sender,
		&senderName,
		&email.Recipient,
		&email.Subject,
		&email.BodyHTML,
		&email.BodyText,
		&email.Status,
		&errMsg,
		&email.CreatedAt,
		&metadataBytes,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if userID.Valid {
		val := userID.String
		email.UserID = &val
	}
	if senderName.Valid {
		email.SenderName = senderName.String
	}
	if errMsg.Valid {
		email.ErrorMessage = errMsg.String
	}
	if len(metadataBytes) > 0 {
		email.Metadata = string(metadataBytes)
	}

	return &email, nil
}
