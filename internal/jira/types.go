// Package jira contains the HTTP client for the Jira Cloud REST API v3 and
// the domain types used by the rest of ripjira. UI code consumes only these
// domain types — raw API DTOs never leave this package.
package jira

import "time"

// User identifies a Jira account.
type User struct {
	AccountID   string
	DisplayName string
	Email       string
}

// Status represents the workflow state of an issue.
type Status struct {
	ID       string
	Name     string
	Category string // "new" | "indeterminate" | "done"
}

// Priority represents an issue priority.
type Priority struct {
	ID   string
	Name string
}

// IssueType represents the issue type (Task, Bug, Story, …).
type IssueType struct {
	ID      string
	Name    string
	Subtask bool
}

// SubtaskRef is a lightweight reference to a child issue, sourced from
// the parent's `fields.subtasks` array. Only the fields needed for
// display are populated.
type SubtaskRef struct {
	Key     string
	Summary string
	Status  Status
}

// Comment is a single comment on an issue.
type Comment struct {
	ID      string
	Author  User
	Body    string // plain text
	Created time.Time
}

// Transition is an available workflow transition.
type Transition struct {
	ID   string
	Name string
	To   Status
}

// Project is a Jira project as returned by /project/search.
type Project struct {
	ID   string
	Key  string
	Name string
}

// FieldOption is an entry in a field's allowedValues list. Name carries the
// human-readable label (Jira returns this under either "name" or "value").
type FieldOption struct {
	ID   string
	Name string
}

// FieldMeta describes a single field on the createmeta response. SchemaType is
// the wire schema.type (e.g. "string", "option", "priority", "user", "array",
// "number", "date", "datetime"); when SchemaType is "array", SchemaItems holds
// schema.items.
type FieldMeta struct {
	ID            string
	Name          string
	Required      bool
	SchemaType    string
	SchemaItems   string
	AllowedValues []FieldOption
}

// CreateMeta is the per-(project, issuetype) field metadata returned by
// /issue/createmeta/{key}/issuetypes/{typeId}.
type CreateMeta struct {
	Fields []FieldMeta
}

// CreatePayload is the input to CreateIssue. ProjectKey, IssueTypeID and
// Summary are required; all other fields are optional. Description is plain
// text and is converted to ADF before being sent. Fields holds any additional
// raw values (custom fields, etc.) which are merged verbatim into the request
// body under "fields".
type CreatePayload struct {
	ProjectKey  string
	IssueTypeID string
	Summary     string
	Description string
	Priority    string
	Assignee    string
	Labels      []string
	ParentKey   string
	Fields      map[string]any
}

// Attachment is a file attached to an issue. Content is the absolute URL
// for the binary; Thumbnail is a smaller pre-rendered preview URL the Jira
// API supplies for image attachments (empty for non-images).
type Attachment struct {
	ID        string
	Filename  string
	MimeType  string
	Size      int64
	Content   string
	Thumbnail string
}

// Issue is the domain model for a Jira issue.
type Issue struct {
	Key         string
	Summary     string
	Status      Status
	Priority    Priority
	Type        IssueType
	Assignee    *User
	Reporter    *User
	Description string // markdown converted from renderedFields HTML
	Comments    []Comment
	Subtasks    []SubtaskRef
	Attachments []Attachment
	Created     time.Time
	Updated     time.Time
	Transitions []Transition
	URL         string // baseURL + "/browse/" + Key
}
