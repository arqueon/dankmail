// Package contacts feeds the compose autocomplete from two sources:
// correspondents inferred from the cached mail (no permissions needed)
// and Google Contacts via the People API (contacts scopes + re-consent).
package contacts

import (
	"context"
	"net/http"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	peoplev1 "google.golang.org/api/people/v1"

	"github.com/arqueon/dankmail/core/ent"
	entaccount "github.com/arqueon/dankmail/core/ent/account"
	"github.com/arqueon/dankmail/core/ent/contact"
	"github.com/arqueon/dankmail/core/ent/message"
	"github.com/arqueon/dankmail/core/ent/thread"
)

// entry is an aggregation bucket during mail indexing.
type entry struct {
	name     string
	weight   int
	lastSeen int64
}

// IndexMail rebuilds the source=mail contact rows for every account from
// the cached messages (From/To/Cc). Cheap full rescan; run periodically.
func IndexMail(ctx context.Context, db *ent.Client) error {
	accts, err := db.Account.Query().All(ctx)
	if err != nil {
		return err
	}
	for _, acct := range accts {
		msgs, err := db.Message.Query().
			Where(message.HasThreadWith(thread.HasAccountWith(entaccount.IDEQ(acct.ID)))).
			Select(message.FieldFrom, message.FieldTo, message.FieldCc, message.FieldDate).
			All(ctx)
		if err != nil {
			return err
		}
		agg := map[string]*entry{}
		self := strings.ToLower(acct.Email)
		bump := func(raw string, when int64, w int) {
			name, email := splitAddr(raw)
			if email == "" || email == self || isNoReply(email) {
				return
			}
			e := agg[email]
			if e == nil {
				e = &entry{}
				agg[email] = e
			}
			e.weight += w
			if name != "" && (e.name == "" || len(name) > len(e.name)) {
				e.name = name
			}
			if when > e.lastSeen {
				e.lastSeen = when
			}
		}
		for _, m := range msgs {
			when := m.Date.Unix()
			// People who write to you rank higher than mere co-recipients.
			bump(m.From, when, 3)
			for _, r := range m.To {
				bump(r, when, 1)
			}
			for _, r := range m.Cc {
				bump(r, when, 1)
			}
		}
		for email, e := range agg {
			if err := upsertContact(ctx, db, acct.ID, email, e.name, contact.SourceMail, e.weight, timeFromUnix(e.lastSeen)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrInsufficientScope marks tokens granted before the contacts scopes
// existed: the account needs a re-consent to unlock Google contacts.
type insufficientScopeError struct{ inner error }

func (e *insufficientScopeError) Error() string {
	return "contacts scopes not granted: " + e.inner.Error()
}
func (e *insufficientScopeError) Unwrap() error { return e.inner }

// IsInsufficientScope reports whether err means "re-consent required".
func IsInsufficientScope(err error) bool {
	_, ok := err.(*insufficientScopeError)
	return ok
}

// FetchGoogle pulls the account's People API contacts ("connections")
// and the autocomplete pool ("other contacts") into source=google rows.
func FetchGoogle(ctx context.Context, db *ent.Client, accountID uuid.UUID, hc *http.Client) error {
	svc, err := peoplev1.NewService(ctx, option.WithHTTPClient(hc))
	if err != nil {
		return err
	}

	upsert := func(name, email string) error {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			return nil
		}
		// Weight 5: above casual mail co-recipients.
		return upsertContact(ctx, db, accountID, email, name, contact.SourceGoogle, 5, time.Now().UTC())
	}
	ingest := func(people []*peoplev1.Person) error {
		for _, p := range people {
			name := ""
			if len(p.Names) > 0 {
				name = p.Names[0].DisplayName
			}
			for _, e := range p.EmailAddresses {
				if err := upsert(name, e.Value); err != nil {
					return err
				}
			}
		}
		return nil
	}

	pageToken := ""
	for {
		call := svc.OtherContacts.List().ReadMask("names,emailAddresses").PageSize(1000).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return classifyPeopleErr(err)
		}
		if err := ingest(resp.OtherContacts); err != nil {
			return err
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	pageToken = ""
	for {
		call := svc.People.Connections.List("people/me").
			PersonFields("names,emailAddresses").PageSize(1000).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return classifyPeopleErr(err)
		}
		if err := ingest(resp.Connections); err != nil {
			return err
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return nil
}

func classifyPeopleErr(err error) error {
	var gerr *googleapi.Error
	if ok := asGoogleErr(err, &gerr); ok && (gerr.Code == 403 || gerr.Code == 401) {
		return &insufficientScopeError{inner: err}
	}
	return err
}

func asGoogleErr(err error, target **googleapi.Error) bool {
	for err != nil {
		if g, ok := err.(*googleapi.Error); ok {
			*target = g
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// Suggestion is one autocomplete candidate.
type Suggestion struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Source string `json:"source"`
}

// Search returns ranked suggestions matching prefix (name or email),
// deduped across sources (google names win; mail weights add up).
func Search(ctx context.Context, db *ent.Client, accountID *uuid.UUID, query string, limit int) ([]Suggestion, error) {
	if limit <= 0 {
		limit = 8
	}
	q := db.Contact.Query()
	if accountID != nil {
		q = q.Where(contact.HasAccountWith(entaccount.IDEQ(*accountID)))
	}
	if query != "" {
		q = q.Where(contact.Or(
			contact.EmailContainsFold(query),
			contact.NameContainsFold(query),
		))
	}
	rows, err := q.Order(ent.Desc(contact.FieldWeight), ent.Desc(contact.FieldLastSeen)).
		Limit(limit * 4). // room for cross-source dedupe
		All(ctx)
	if err != nil {
		return nil, err
	}

	type merged struct {
		Suggestion
		weight int
	}
	byEmail := map[string]*merged{}
	order := []string{}
	for _, r := range rows {
		m := byEmail[r.Email]
		if m == nil {
			m = &merged{Suggestion: Suggestion{Email: r.Email}}
			byEmail[r.Email] = m
			order = append(order, r.Email)
		}
		m.weight += r.Weight
		if r.Name != "" && (m.Name == "" || r.Source == contact.SourceGoogle) {
			m.Name = r.Name
		}
		if m.Source == "" || r.Source == contact.SourceGoogle {
			m.Source = string(r.Source)
		}
	}
	out := make([]Suggestion, 0, len(order))
	for _, email := range order {
		out = append(out, byEmail[email].Suggestion)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return byEmail[out[i].Email].weight > byEmail[out[j].Email].weight
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// upsertContact is a manual upsert on the (account, email, source)
// unique key — portable across dialects, tiny volumes.
func upsertContact(ctx context.Context, db *ent.Client, accountID uuid.UUID, email, name string, source contact.Source, weight int, lastSeen time.Time) error {
	existing, err := db.Contact.Query().
		Where(
			contact.HasAccountWith(entaccount.IDEQ(accountID)),
			contact.EmailEQ(email),
			contact.SourceEQ(source),
		).
		Only(ctx)
	switch {
	case ent.IsNotFound(err):
		_, err = db.Contact.Create().
			SetAccountID(accountID).
			SetEmail(email).
			SetName(name).
			SetSource(source).
			SetWeight(weight).
			SetLastSeen(lastSeen).
			Save(ctx)
		return err
	case err != nil:
		return err
	}
	u := db.Contact.UpdateOne(existing).SetWeight(weight).SetLastSeen(lastSeen)
	if name != "" {
		u.SetName(name)
	}
	_, err = u.Save(ctx)
	return err
}

// isNoReply filters machine senders out of the suggestion pool.
func isNoReply(email string) bool {
	local := strings.SplitN(email, "@", 2)[0]
	for _, marker := range []string{"noreply", "no-reply", "no_reply", "donotreply", "do-not-reply", "notifications", "notificaciones", "mailer-daemon"} {
		if strings.Contains(local, marker) {
			return true
		}
	}
	return false
}

func timeFromUnix(sec int64) time.Time {
	if sec <= 0 {
		return time.Unix(0, 0).UTC()
	}
	return time.Unix(sec, 0).UTC()
}

func splitAddr(raw string) (name, email string) {
	a, err := mail.ParseAddress(raw)
	if err != nil {
		s := strings.ToLower(strings.TrimSpace(raw))
		if strings.Contains(s, "@") {
			return "", s
		}
		return "", ""
	}
	return strings.TrimSpace(a.Name), strings.ToLower(a.Address)
}
