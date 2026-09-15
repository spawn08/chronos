package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spawn08/chronos/storage"
)

const summaryRecentMessages = 32

// Match strings.TrimSpace, including Unicode whitespace, before projecting.
const summaryWhitespace = `char(9,10,11,12,13,32,133,160,5760,8192,8193,8194,8195,8196,8197,8198,8199,8200,8201,8202,8232,8233,8239,8287,12288)`

const readSummaryQuery = `
		SELECT seq_num,
		       substr(CAST(trim(json_extract(payload, '$.summary'), ` + summaryWhitespace + `) AS BLOB), 1, ?),
		       length(CAST(trim(json_extract(payload, '$.summary'), ` + summaryWhitespace + `) AS BLOB)) > ?
		FROM events
		WHERE tenant_id=? AND session_id=? AND type='chat_summary'
		  AND CASE WHEN json_valid(payload) THEN
		      json_type(payload, '$.summary')='text'
		      AND trim(json_extract(payload, '$.summary'), ` + summaryWhitespace + `) != ''
		      ELSE 0 END
		ORDER BY seq_num DESC, id DESC LIMIT 1`

const readSummaryMessagesQuery = `
		SELECT seq_num, json_extract(payload, '$.role'),
		       substr(CAST(trim(json_extract(payload, '$.content'), ` + summaryWhitespace + `) AS BLOB), 1, ?),
		       length(CAST(trim(json_extract(payload, '$.content'), ` + summaryWhitespace + `) AS BLOB)) > ?
		FROM events
		WHERE tenant_id=? AND session_id=? AND type='chat_message'
		  AND CASE WHEN json_valid(payload) THEN
		      json_extract(payload, '$.role') IN ('user', 'assistant')
		      AND json_type(payload, '$.content')='text'
		      AND trim(json_extract(payload, '$.content'), ` + summaryWhitespace + `) != ''
		      ELSE 0 END
		ORDER BY seq_num DESC, id DESC LIMIT ?`

// ReadSessionSummary returns the latest nonempty chat_summary, or up to 32 recent
// user/assistant messages in chronological order when no summary exists. Text is
// valid UTF-8 and at most maxBytes bytes. Empty text with no error means no usable
// context (or a nonpositive budget). source is chat_summary or chat_message;
// sourceSeq is the latest selected event sequence. truncated reports omitted text
// or older fallback messages. All reads are scoped to the context tenant.
//
// This additive capability deliberately uses built-in return types so consumers
// can detect it structurally without extending storage.Storage. SQL projects only
// bounded text and scalar metadata, never raw event payloads into Go.
func (s *Store) ReadSessionSummary(ctx context.Context, sessionID string, maxBytes int) (text, source string, sourceSeq int64, truncated bool, err error) {
	if maxBytes <= 0 {
		return "", "", 0, false, nil
	}
	tenant := storage.TenantFromContext(ctx)
	err = s.db.QueryRowContext(ctx, readSummaryQuery, maxBytes, maxBytes, tenant, sessionID).Scan(&sourceSeq, &text, &truncated)
	if err == nil {
		return summaryUTF8(text), "chat_summary", sourceSeq, truncated, nil
	}
	if err != sql.ErrNoRows {
		return "", "", 0, false, fmt.Errorf("read session summary: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, readSummaryMessagesQuery, maxBytes, maxBytes, tenant, sessionID, summaryRecentMessages+1)
	if err != nil {
		return "", "", 0, false, fmt.Errorf("read session summary messages: %w", err)
	}
	defer rows.Close()
	var parts []string
	remaining := maxBytes
	for rows.Next() {
		if len(parts) == summaryRecentMessages || remaining <= 1 && len(parts) > 0 {
			truncated = true
			break
		}
		var seq int64
		var role, content string
		var clipped bool
		if err := rows.Scan(&seq, &role, &content, &clipped); err != nil {
			return "", "", 0, false, fmt.Errorf("scan session summary message: %w", err)
		}
		if len(parts) == 0 {
			sourceSeq = seq
		} else {
			remaining-- // separator between messages
		}
		part := role + ": " + summaryUTF8(content)
		if len(part) > remaining {
			part = summaryUTF8(part[:remaining])
			clipped = true
		}
		parts = append(parts, part)
		remaining -= len(part)
		truncated = truncated || clipped
	}
	if err := rows.Err(); err != nil {
		return "", "", 0, false, fmt.Errorf("read session summary messages: %w", err)
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	if len(parts) == 0 {
		return "", "", 0, false, nil
	}
	return strings.Join(parts, "\n"), "chat_message", sourceSeq, truncated, nil
}

func summaryUTF8(text string) string {
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}
