package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/arqueon/dankmail/core/ent"
	"github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/ent/message"
	"github.com/arqueon/dankmail/core/ent/pendingop"
	"github.com/arqueon/dankmail/core/ent/thread"
	"github.com/arqueon/dankmail/core/models"
)

// ThreadFilter narrows ListThreads.
type ThreadFilter struct {
	AccountID  *uuid.UUID
	UnreadOnly bool
	Starred    bool
	InboxOnly  bool
	// Query matches subject, snippet, sender, or message body
	// (case-folded LIKE over the local cache — FTS5 is a ring-3
	// upgrade). While searching, snoozed threads are included.
	Query string
	Limit int
}

// ListThreads returns the unified triage list, newest first. Snoozed
// threads are hidden (they come back when they wake) except when
// searching.
func (r *Repo) ListThreads(ctx context.Context, f ThreadFilter) ([]models.ThreadSummary, error) {
	q := r.client.Thread.Query().
		WithAccount().
		Order(ent.Desc(thread.FieldLastMessageAt))
	if f.Query == "" {
		q = q.Where(thread.SnoozedUntilIsNil())
	} else {
		q = q.Where(thread.Or(
			thread.SubjectContainsFold(f.Query),
			thread.SnippetContainsFold(f.Query),
			thread.HasMessagesWith(message.Or(
				message.FromContainsFold(f.Query),
				message.BodyTextContainsFold(f.Query),
			)),
		))
	}
	if f.AccountID != nil {
		q = q.Where(thread.HasAccountWith(account.IDEQ(*f.AccountID)))
	}
	if f.UnreadOnly {
		q = q.Where(thread.UnreadEQ(true))
	}
	if f.Starred {
		q = q.Where(thread.StarredEQ(true))
	}
	if f.InboxOnly {
		q = q.Where(thread.InInboxEQ(true))
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := q.Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.ThreadSummary, 0, len(rows))
	for _, t := range rows {
		out = append(out, threadSummary(t))
	}
	return out, nil
}

// GetThread returns one thread with its messages, oldest first.
func (r *Repo) GetThread(ctx context.Context, id int) (*models.ThreadDetail, error) {
	t, err := r.client.Thread.Query().
		Where(thread.IDEQ(id)).
		WithAccount().
		WithMessages(func(q *ent.MessageQuery) { q.Order(ent.Asc(message.FieldDate)) }).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	d := &models.ThreadDetail{ThreadSummary: threadSummary(t)}
	for _, m := range t.Edges.Messages {
		mv := models.MessageView{
			ID:                m.ID,
			ProviderMessageID: m.ProviderMessageID,
			From:              m.From,
			To:                m.To,
			Cc:                m.Cc,
			Date:              m.Date,
			Snippet:           m.Snippet,
			BodyText:          m.BodyText,
		}
		for _, a := range m.Attachments {
			mv.Attachments = append(mv.Attachments, models.AttachmentView{
				Filename: a.Filename, MimeType: a.MimeType, Size: a.Size,
			})
		}
		d.Messages = append(d.Messages, mv)
	}
	return d, nil
}

// UnreadCount aggregates unread inbox threads, optionally per account.
func (r *Repo) UnreadCount(ctx context.Context, accountID *uuid.UUID) (int, error) {
	q := r.client.Thread.Query().
		Where(thread.UnreadEQ(true), thread.InInboxEQ(true), thread.SnoozedUntilIsNil())
	if accountID != nil {
		q = q.Where(thread.HasAccountWith(account.IDEQ(*accountID)))
	}
	return q.Count(ctx)
}

// Accounts returns all accounts with their unread counters.
func (r *Repo) Accounts(ctx context.Context) ([]models.AccountView, error) {
	rows, err := r.client.Account.Query().Order(ent.Asc(account.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.AccountView, 0, len(rows))
	for _, a := range rows {
		unread, err := r.UnreadCount(ctx, &a.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, models.AccountView{
			ID:          a.ID.String(),
			Type:        string(a.Type),
			Email:       a.Email,
			DisplayName: a.DisplayName,
			Status:      string(a.Status),
			LastError:   a.LastError,
			LastSyncAt:  a.LastSyncAt,
			Unread:      unread,
		})
	}
	return out, nil
}

// QueueStats counts pending ops by state.
func (r *Repo) QueueStats(ctx context.Context) (models.QueueStats, error) {
	var s models.QueueStats
	var err error
	if s.Pending, err = r.client.PendingOp.Query().Where(pendingop.StateEQ(pendingop.StatePending)).Count(ctx); err != nil {
		return s, err
	}
	if s.Inflight, err = r.client.PendingOp.Query().Where(pendingop.StateEQ(pendingop.StateInflight)).Count(ctx); err != nil {
		return s, err
	}
	if s.Failed, err = r.client.PendingOp.Query().Where(pendingop.StateEQ(pendingop.StateFailed)).Count(ctx); err != nil {
		return s, err
	}
	return s, nil
}

func threadSummary(t *ent.Thread) models.ThreadSummary {
	s := models.ThreadSummary{
		ID:               t.ID,
		ProviderThreadID: t.ProviderThreadID,
		Subject:          t.Subject,
		Snippet:          t.Snippet,
		LastMessageAt:    t.LastMessageAt,
		Participants:     t.Participants,
		Unread:           t.Unread,
		Starred:          t.Starred,
		InInbox:          t.InInbox,
		SnoozedUntil:     t.SnoozedUntil,
		MessageCount:     t.MessageCount,
		HasAttachments:   t.HasAttachments,
	}
	if t.Edges.Account != nil {
		s.AccountID = t.Edges.Account.ID.String()
	}
	return s
}
