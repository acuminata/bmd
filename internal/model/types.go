package model

// Entry mirrors one element of the "entries" array returned by both
// the marriages and impediments endpoints.
type Entry struct {
	ApplicationNumber string `json:"application_number"`
	BrideGroomsName   string `json:"bride_grooms_name"`
	BrideGroomsPhoto  string `json:"bride_grooms_photo"`
	BridesName        string `json:"brides_name"`
	BridesPhoto       string `json:"brides_photo"`
	CreatedBy         string `json:"created_by"`
	DateOfMarriage    string `json:"date_of_marriage"`
	DateOfNotice      string `json:"date_of_notice"`
	ID                int64  `json:"id"`
	PlaceOfMarriage   string `json:"place_of_marriage"`
	SubmittedAt       string `json:"submitted_at"`
}

// PageResponse mirrors the paginated envelope:
// {"entries":[...],"page_number":6,"page_size":30,"total_entries":3233,"total_pages":108}
type PageResponse struct {
	Entries      []Entry `json:"entries"`
	PageNumber   int     `json:"page_number"`
	PageSize     int     `json:"page_size"`
	TotalEntries int     `json:"total_entries"`
	TotalPages   int     `json:"total_pages"`
}
