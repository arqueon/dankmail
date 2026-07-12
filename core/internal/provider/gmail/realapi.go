package gmail

import (
	"context"
	"encoding/base64"
	"net/http"

	"github.com/arqueon/dankmail/core/errdefs"
	gmailv1 "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// userID is the special Gmail user id meaning "the authenticated account".
const userID = "me"

// metadataHeaders are the headers requested with format=metadata: exactly
// what messageDelta and mailmime.BuildReply need.
var metadataHeaders = []string{
	"Subject", "From", "To", "Cc", "Reply-To", "Message-ID", "References",
}

// realAPI implements gmailAPI over google.golang.org/api/gmail/v1.
// It returns raw errors; classification happens in the Provider.
type realAPI struct {
	svc *gmailv1.Service
}

// newRealAPI builds the seam over an OAuth-authenticated *http.Client
// (wired later by the oauth broker).
func newRealAPI(hc *http.Client) (*realAPI, error) {
	svc, err := gmailv1.NewService(context.Background(), option.WithHTTPClient(hc))
	if err != nil {
		return nil, err
	}
	return &realAPI{svc: svc}, nil
}

// NewWithClient builds a Provider over the real Gmail API using an
// OAuth-authenticated HTTP client.
func NewWithClient(accountID, email string, hc *http.Client, opts Options) (*Provider, error) {
	api, err := newRealAPI(hc)
	if err != nil {
		return nil, errdefs.Wrap(errdefs.KindPermanent, err)
	}
	return New(accountID, email, api, opts), nil
}

func (r *realAPI) ListThreads(ctx context.Context, labelIDs []string, pageToken string) ([]string, string, error) {
	call := r.svc.Users.Threads.List(userID).Context(ctx)
	if len(labelIDs) > 0 {
		call = call.LabelIds(labelIDs...)
	}
	if pageToken != "" {
		call = call.PageToken(pageToken)
	}
	resp, err := call.Do()
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(resp.Threads))
	for _, t := range resp.Threads {
		ids = append(ids, t.Id)
	}
	return ids, resp.NextPageToken, nil
}

func (r *realAPI) GetThread(ctx context.Context, id string) (*gmailv1.Thread, error) {
	return r.svc.Users.Threads.Get(userID, id).Format("full").Context(ctx).Do()
}

func (r *realAPI) GetMessageMetadata(ctx context.Context, id string) (*gmailv1.Message, error) {
	return r.svc.Users.Messages.Get(userID, id).
		Format("metadata").
		MetadataHeaders(metadataHeaders...).
		Context(ctx).Do()
}

func (r *realAPI) ListHistory(ctx context.Context, startHistoryID uint64, pageToken string) (*gmailv1.ListHistoryResponse, error) {
	call := r.svc.Users.History.List(userID).StartHistoryId(startHistoryID).Context(ctx)
	if pageToken != "" {
		call = call.PageToken(pageToken)
	}
	return call.Do()
}

func (r *realAPI) ModifyThread(ctx context.Context, threadID string, addLabelIDs, removeLabelIDs []string) error {
	_, err := r.svc.Users.Threads.Modify(userID, threadID, &gmailv1.ModifyThreadRequest{
		AddLabelIds:    addLabelIDs,
		RemoveLabelIds: removeLabelIDs,
	}).Context(ctx).Do()
	return err
}

func (r *realAPI) SendMessage(ctx context.Context, threadID string, raw []byte) error {
	msg := &gmailv1.Message{Raw: base64.RawURLEncoding.EncodeToString(raw)}
	if threadID != "" {
		msg.ThreadId = threadID
	}
	_, err := r.svc.Users.Messages.Send(userID, msg).Context(ctx).Do()
	return err
}

func (r *realAPI) SearchThreads(ctx context.Context, query string, pageToken string) ([]string, string, error) {
	call := r.svc.Users.Threads.List(userID).Q(query).IncludeSpamTrash(true).Context(ctx)
	if pageToken != "" {
		call = call.PageToken(pageToken)
	}
	resp, err := call.Do()
	if err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(resp.Threads))
	for _, t := range resp.Threads {
		ids = append(ids, t.Id)
	}
	return ids, resp.NextPageToken, nil
}

func (r *realAPI) GetProfile(ctx context.Context) (string, uint64, error) {
	p, err := r.svc.Users.GetProfile(userID).Context(ctx).Do()
	if err != nil {
		return "", 0, err
	}
	return p.EmailAddress, p.HistoryId, nil
}
