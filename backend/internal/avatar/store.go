package avatar

import (
	"context"
	"database/sql"
	"strings"
)

func Get(ctx context.Context, db *sql.DB, hash string) (Row, bool, error) {
	var row Row
	var image []byte
	var mime, etag, fetched string
	var byteSize sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT email_hash, status, image, COALESCE(mime_type, ''), byte_size, COALESCE(upstream_etag, ''), fetched_at
		FROM memento_avatar
		WHERE email_hash = ?`, hash).Scan(
		&row.EmailHash, &row.Status, &image, &mime, &byteSize, &etag, &fetched,
	)
	if err == sql.ErrNoRows {
		return Row{}, false, nil
	}
	if err != nil {
		return Row{}, false, err
	}
	row.Image = image
	row.MimeType = mime
	row.UpstreamETag = etag
	row.FetchedAt = fetched
	if byteSize.Valid {
		row.ByteSize = byteSize.Int64
	}
	return row, true, nil
}

func Put(ctx context.Context, db *sql.DB, row Row) error {
	if row.Status == StatusFound && row.ByteSize == 0 {
		row.ByteSize = int64(len(row.Image))
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO memento_avatar(email_hash, status, image, mime_type, byte_size, upstream_etag, fetched_at)
		VALUES(?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(email_hash) DO UPDATE SET
			status = excluded.status,
			image = excluded.image,
			mime_type = excluded.mime_type,
			byte_size = excluded.byte_size,
			upstream_etag = excluded.upstream_etag,
			fetched_at = excluded.fetched_at`,
		row.EmailHash,
		string(row.Status),
		nullableBytes(row),
		nullableString(row.MimeType, row.Status == StatusFound),
		nullableInt(row.ByteSize, row.Status == StatusFound),
		nullableString(row.UpstreamETag, row.UpstreamETag != ""),
	)
	return err
}

func KnownHash(ctx context.Context, db *sql.DB, hash string) (bool, error) {
	known, err := KnownHashes(ctx, db)
	if err != nil {
		return false, err
	}
	for _, item := range known {
		if item.EmailHash == hash {
			return true, nil
		}
	}
	return false, nil
}

func KnownHashes(ctx context.Context, db *sql.DB) ([]KnownAvatar, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT DISTINCT email FROM (
			SELECT email_address AS email FROM memento_person_email WHERE COALESCE(email_address, '') <> ''
			UNION
			SELECT primary_email AS email FROM memento_person WHERE COALESCE(primary_email, '') <> ''
			UNION
			SELECT value AS email FROM memento_config WHERE key = 'owner_email' AND COALESCE(value, '') <> ''
		)
		ORDER BY lower(email)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnownAvatar
	seen := map[string]bool{}
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		email = NormalizeEmail(email)
		if email == "" || !strings.Contains(email, "@") || seen[email] {
			continue
		}
		seen[email] = true
		out = append(out, KnownAvatar{Email: email, EmailHash: HashEmail(email)})
	}
	return out, rows.Err()
}

func nullableBytes(row Row) any {
	if row.Status != StatusFound {
		return nil
	}
	return row.Image
}

func nullableString(s string, ok bool) any {
	if !ok {
		return nil
	}
	return s
}

func nullableInt(n int64, ok bool) any {
	if !ok {
		return nil
	}
	return n
}
