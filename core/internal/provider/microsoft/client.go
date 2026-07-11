package microsoft

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"

	"github.com/arqueon/dankmail/core/internal/provider"
)

const graphBase = "https://graph.microsoft.com/v1.0"

// deltaSelect keeps folder deltas light: no bodies (conversations are
// re-fetched whole when affected).
const deltaSelect = "id,conversationId,receivedDateTime,isRead,flag,parentFolderId"

// convSelect is the full message shape used by ListConversation and
// GetMessage: body + reply headers included.
const convSelect = "id,conversationId,internetMessageId,subject,bodyPreview,from,toRecipients,ccRecipients,receivedDateTime,isRead,flag,parentFolderId,hasAttachments,body,internetMessageHeaders"

// Client is the real graphAPI over HTTP; hc must inject OAuth (an
// oauth2.NewClient built from the persisting token source).
type Client struct {
	hc *http.Client
}

func NewClient(hc *http.Client) *Client { return &Client{hc: hc} }

// --- wire types -------------------------------------------------------

type wireRecipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

func (r wireRecipient) display() string {
	if r.EmailAddress.Name != "" && r.EmailAddress.Name != r.EmailAddress.Address {
		return fmt.Sprintf("%s <%s>", r.EmailAddress.Name, r.EmailAddress.Address)
	}
	return r.EmailAddress.Address
}

type wireMessage struct {
	ID                string          `json:"id"`
	ConversationID    string          `json:"conversationId"`
	InternetMessageID string          `json:"internetMessageId"`
	Subject           string          `json:"subject"`
	BodyPreview       string          `json:"bodyPreview"`
	From              *wireRecipient  `json:"from"`
	ToRecipients      []wireRecipient `json:"toRecipients"`
	CcRecipients      []wireRecipient `json:"ccRecipients"`
	ReceivedDateTime  time.Time       `json:"receivedDateTime"`
	IsRead            bool            `json:"isRead"`
	Flag              *struct {
		FlagStatus string `json:"flagStatus"`
	} `json:"flag"`
	ParentFolderID string `json:"parentFolderId"`
	HasAttachments bool   `json:"hasAttachments"`
	Body           *struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	InternetMessageHeaders []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"internetMessageHeaders"`
	Removed *struct {
		Reason string `json:"reason"`
	} `json:"@removed"`
}

func (w *wireMessage) toGraphMessage() *graphMessage {
	m := &graphMessage{
		ID:                w.ID,
		ConversationID:    w.ConversationID,
		InternetMessageID: w.InternetMessageID,
		Subject:           w.Subject,
		BodyPreview:       w.BodyPreview,
		ReceivedAt:        w.ReceivedDateTime.Unix(),
		IsRead:            w.IsRead,
		ParentFolderID:    w.ParentFolderID,
		HasAttachments:    w.HasAttachments,
		Removed:           w.Removed != nil,
	}
	if w.From != nil {
		m.From = w.From.display()
	}
	for _, r := range w.ToRecipients {
		m.To = append(m.To, r.display())
	}
	for _, r := range w.CcRecipients {
		m.Cc = append(m.Cc, r.display())
	}
	if w.Flag != nil && w.Flag.FlagStatus == "flagged" {
		m.Flagged = true
	}
	if w.Body != nil {
		if strings.EqualFold(w.Body.ContentType, "html") {
			m.BodyText = htmlToText(w.Body.Content)
		} else {
			m.BodyText = strings.TrimSpace(w.Body.Content)
		}
	}
	if len(w.InternetMessageHeaders) > 0 {
		m.Headers = map[string]string{}
		for _, h := range w.InternetMessageHeaders {
			m.Headers[http.CanonicalHeaderKey(h.Name)] = h.Value
		}
	}
	return m
}

// --- request plumbing -------------------------------------------------

func (c *Client) do(ctx context.Context, method, u string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var doc struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = json.Unmarshal(raw, &doc)
		return &graphError{Status: resp.StatusCode, Code: doc.Error.Code, Msg: doc.Error.Message}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- graphAPI ---------------------------------------------------------

func (c *Client) GetProfile(ctx context.Context) (string, error) {
	var doc struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
	}
	if err := c.do(ctx, http.MethodGet, graphBase+"/me?$select=mail,userPrincipalName", nil, &doc); err != nil {
		return "", err
	}
	if doc.Mail != "" {
		return doc.Mail, nil
	}
	return doc.UserPrincipalName, nil
}

func (c *Client) DeltaMessages(ctx context.Context, folder, link string) (deltaPage, error) {
	u := link
	if u == "" {
		u = fmt.Sprintf("%s/me/mailFolders/%s/messages/delta?$select=%s", graphBase, folder, url.QueryEscape(deltaSelect))
	}
	var doc struct {
		Value     []wireMessage `json:"value"`
		NextLink  string        `json:"@odata.nextLink"`
		DeltaLink string        `json:"@odata.deltaLink"`
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &doc); err != nil {
		return deltaPage{}, err
	}
	page := deltaPage{NextLink: doc.NextLink, DeltaLink: doc.DeltaLink}
	for i := range doc.Value {
		page.Messages = append(page.Messages, doc.Value[i].toGraphMessage())
	}
	return page, nil
}

func (c *Client) GetMessage(ctx context.Context, id string) (*graphMessage, error) {
	var doc wireMessage
	u := fmt.Sprintf("%s/me/messages/%s?$select=%s", graphBase, url.PathEscape(id), url.QueryEscape(convSelect))
	if err := c.do(ctx, http.MethodGet, u, nil, &doc); err != nil {
		return nil, err
	}
	m := doc.toGraphMessage()
	if err := c.fillAttachments(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *Client) ListConversation(ctx context.Context, convID string) ([]*graphMessage, error) {
	// conversationId needs quoting inside the OData filter.
	filter := fmt.Sprintf("conversationId eq '%s'", strings.ReplaceAll(convID, "'", "''"))
	u := fmt.Sprintf("%s/me/messages?$filter=%s&$select=%s&$top=100",
		graphBase, url.QueryEscape(filter), url.QueryEscape(convSelect))
	var out []*graphMessage
	for u != "" {
		var doc struct {
			Value    []wireMessage `json:"value"`
			NextLink string        `json:"@odata.nextLink"`
		}
		if err := c.do(ctx, http.MethodGet, u, nil, &doc); err != nil {
			return nil, err
		}
		for i := range doc.Value {
			m := doc.Value[i].toGraphMessage()
			if err := c.fillAttachments(ctx, m); err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		u = doc.NextLink
	}
	return out, nil
}

// fillAttachments fetches attachment METADATA only (name, type, size —
// spec §1: content never leaves the provider).
func (c *Client) fillAttachments(ctx context.Context, m *graphMessage) error {
	if !m.HasAttachments {
		return nil
	}
	u := fmt.Sprintf("%s/me/messages/%s/attachments?$select=name,contentType,size", graphBase, url.PathEscape(m.ID))
	var doc struct {
		Value []struct {
			Name        string `json:"name"`
			ContentType string `json:"contentType"`
			Size        int64  `json:"size"`
		} `json:"value"`
	}
	if err := c.do(ctx, http.MethodGet, u, nil, &doc); err != nil {
		return err
	}
	for _, a := range doc.Value {
		m.Attachments = append(m.Attachments, provider.AttachmentMeta{
			Filename: a.Name, MimeType: a.ContentType, Size: a.Size,
		})
	}
	return nil
}

func (c *Client) PatchMessage(ctx context.Context, id string, body map[string]any) error {
	u := fmt.Sprintf("%s/me/messages/%s", graphBase, url.PathEscape(id))
	return c.do(ctx, http.MethodPatch, u, body, nil)
}

func (c *Client) MoveMessage(ctx context.Context, id, destFolder string) error {
	u := fmt.Sprintf("%s/me/messages/%s/move", graphBase, url.PathEscape(id))
	return c.do(ctx, http.MethodPost, u, map[string]any{"destinationId": destFolder}, nil)
}

// SendMail posts the raw MIME message (Graph expects it base64-encoded
// with Content-Type text/plain).
func (c *Client) SendMail(ctx context.Context, mime []byte) error {
	u := graphBase + "/me/sendMail"
	encoded := base64.StdEncoding.EncodeToString(mime)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &graphError{Status: resp.StatusCode, Msg: string(raw)}
	}
	return nil
}

func (c *Client) FolderIDs(ctx context.Context) (map[string]string, error) {
	ids := map[string]string{}
	for _, name := range []string{folderInbox, folderJunk, folderArchive, folderTrash} {
		var doc struct {
			ID string `json:"id"`
		}
		u := fmt.Sprintf("%s/me/mailFolders/%s?$select=id", graphBase, name)
		if err := c.do(ctx, http.MethodGet, u, nil, &doc); err != nil {
			return nil, err
		}
		ids[name] = doc.ID
	}
	return ids, nil
}

// htmlToText distills HTML to plain markdown-ish text (same degrade-to-
// empty contract as the Gmail provider).
func htmlToText(html string) string {
	if html == "" {
		return ""
	}
	text, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}
