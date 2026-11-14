package api

// Project mirrors the API's project JSON. Times are RFC 3339 strings.
type Project struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Position    int64      `json:"position"`
	ArchivedAt  *string    `json:"archived_at"`
	CreatedAt   string     `json:"created_at"`
	UpdatedAt   string     `json:"updated_at"`
	Tags        []string   `json:"tags"`
	Tasks       []Task     `json:"tasks"`
	Log         []LogEntry `json:"log"`
}

// Task mirrors the API's task JSON.
type Task struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	Title       string  `json:"title"`
	Done        bool    `json:"done"`
	Position    int64   `json:"position"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// LogEntry mirrors the API's log entry JSON.
type LogEntry struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// Tag mirrors the API's tag JSON.
type Tag struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProjectCount int    `json:"project_count"`
}

// Session is a signed-in device.
type Session struct {
	ID         string `json:"id"`
	DeviceName string `json:"device_name"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
	ExpiresAt  string `json:"expires_at"`
	Current    bool   `json:"current"`
}

// ProjectPatch lists the project fields to change; nil fields are omitted.
type ProjectPatch struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
	Archived    *bool     `json:"archived,omitempty"`
}

// TaskPatch lists the task fields to change; nil fields are omitted.
type TaskPatch struct {
	Title *string `json:"title,omitempty"`
	Done  *bool   `json:"done,omitempty"`
}
