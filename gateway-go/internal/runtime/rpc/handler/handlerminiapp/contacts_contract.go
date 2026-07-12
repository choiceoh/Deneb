package handlerminiapp

// ContactRow is one address-book entry on the wire. The contacts subpackage
// owns lookup and projection behavior; this stable DTO remains the client
// generator source of truth.
//
//deneb:wire
type ContactRow struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones,omitempty"`
	Emails []string `json:"emails,omitempty"`
	Org    string   `json:"org,omitempty"`
}
