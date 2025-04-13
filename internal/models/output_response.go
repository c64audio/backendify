package models

// Result Put here because this is what the server will return out to the calling client
type Result struct {
	Data       CompanyResponse
	StatusCode int
}

type CompanyResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Active      bool   `json:"active"`
	ActiveUntil string `json:"active_until,omitempty"`
}
