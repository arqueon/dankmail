// Package models holds the wire-level DTOs shared by the HTTP API and
// the IPC surface, kept separate from ent entities so storage can evolve
// without breaking the UI contract.
package models

import "time"

type AccountView struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Email       string     `json:"email"`
	DisplayName string     `json:"displayName"`
	Status      string     `json:"status"`
	LastError   string     `json:"lastError,omitempty"`
	LastSyncAt  *time.Time `json:"lastSyncAt,omitempty"`
	Unread      int        `json:"unread"`
}

type ThreadSummary struct {
	ID               int        `json:"id"`
	AccountID        string     `json:"accountId"`
	ProviderThreadID string     `json:"providerThreadId"`
	Subject          string     `json:"subject"`
	Snippet          string     `json:"snippet"`
	LastMessageAt    time.Time  `json:"lastMessageAt"`
	Participants     []string   `json:"participants"`
	Unread           bool       `json:"unread"`
	Starred          bool       `json:"starred"`
	InInbox          bool       `json:"inInbox"`
	SnoozedUntil     *time.Time `json:"snoozedUntil,omitempty"`
	MessageCount     int        `json:"messageCount"`
	WebLink          string     `json:"webLink,omitempty"`
}

type MessageView struct {
	ID                int       `json:"id"`
	ProviderMessageID string    `json:"providerMessageId"`
	From              string    `json:"from"`
	To                []string  `json:"to"`
	Cc                []string  `json:"cc,omitempty"`
	Date              time.Time `json:"date"`
	Snippet           string    `json:"snippet"`
	BodyText          string    `json:"bodyText"`
}

type ThreadDetail struct {
	ThreadSummary
	Messages []MessageView `json:"messages"`
}

type QueueStats struct {
	Pending  int `json:"pending"`
	Inflight int `json:"inflight"`
	Failed   int `json:"failed"`
}

type DaemonStatus struct {
	Version  string        `json:"version"`
	Accounts []AccountView `json:"accounts"`
	Unread   int           `json:"unread"`
	Queue    QueueStats    `json:"queue"`
	DND      bool          `json:"dnd"`
}
