package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/arqueon/dankmail/core/ent/message"
	"github.com/arqueon/dankmail/core/ent/predicate"
)

// External-content indexes avoid storing another copy of cached mail bodies.
// Triggers cover sync, optimistic mutations, pruning and cascading deletes.
// Backfill and trigger installation are atomic, and only run once per index.
// See https://www.sqlite.org/fts5.html#the_trigram_tokenizer.
func migrateSearch(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, index := range []struct{ table, name, columns string }{
		{"threads", "thread_search", "subject, snippet"},
		{"messages", "message_search", `"from", body_text`},
	} {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", index.name).Scan(&exists); err != nil {
			return err
		}
		// Identifiers are constants above, never user input.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(%s, content='%s', content_rowid='id', tokenize='trigram', detail=none, columnsize=0)`,
			index.name, index.columns, index.table)); err != nil {
			return err
		}
		oldValues, newValues := "old.id", "new.id"
		for _, column := range strings.Split(index.columns, ", ") {
			oldValues += ", old." + column
			newValues += ", new." + column
		}
		insert := fmt.Sprintf("INSERT INTO %s(rowid, %s) VALUES (%s);", index.name, index.columns, newValues)
		remove := fmt.Sprintf("INSERT INTO %s(%s, rowid, %s) VALUES ('delete', %s);", index.name, index.name, index.columns, oldValues)
		for _, trigger := range []struct{ suffix, event, body string }{
			{"ai", "INSERT", insert},
			{"ad", "DELETE", remove},
			{"au", "UPDATE OF " + index.columns, remove + insert},
		} {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS %s_%s AFTER %s ON %s BEGIN %s END",
				index.name, trigger.suffix, trigger.event, index.table, trigger.body)); err != nil {
				return err
			}
		}
		if exists == 0 {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s(%s) VALUES ('rebuild')", index.name, index.name)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func searchPredicate(query string) predicate.Thread {
	// LIKE without ESCAPE lets FTS5 use its trigram index. It deliberately
	// admits wildcard candidates; instr then enforces literal matching, so
	// searching for '%' or '_' never changes the existing search semantics.
	// One- and two-character searches retain SQLite's scan fallback.
	query = strings.ToLower(query)
	pattern := "%" + query + "%"
	return func(s *entsql.Selector) {
		s.Where(entsql.ExprP(s.C("id")+` IN (
			SELECT rowid FROM thread_search WHERE subject LIKE ? AND instr(lower(subject), ?) > 0
			UNION SELECT rowid FROM thread_search WHERE snippet LIKE ? AND instr(lower(snippet), ?) > 0
			UNION SELECT `+message.ThreadColumn+` FROM messages WHERE id IN (
				SELECT rowid FROM message_search WHERE "from" LIKE ? AND instr(lower("from"), ?) > 0
				UNION SELECT rowid FROM message_search WHERE body_text LIKE ? AND instr(lower(body_text), ?) > 0
			))`, pattern, query, pattern, query, pattern, query, pattern, query))
	}
}
