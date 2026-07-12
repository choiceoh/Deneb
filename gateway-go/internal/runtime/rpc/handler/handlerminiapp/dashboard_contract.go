package handlerminiapp

// Wire shapes for the native client. The dashboard behavior lives in the
// dashboard subpackage; these contracts remain at the generator's stable root.

// DashboardItem is one classified work item in a lane. RefType + RefID let the
// client open the underlying object (a calendar event, a work-feed card);
// WhenMs is the item's salient time in epoch millis.
//
//deneb:wire
type DashboardItem struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Source   string `json:"source"`
	RefType  string `json:"refType,omitempty"`
	RefID    string `json:"refId,omitempty"`
	WhenMs   int64  `json:"whenMs,omitempty"`
}

// LaneOut is one part's bucket.
//
//deneb:wire
type LaneOut struct {
	Key   string          `json:"key"`
	Name  string          `json:"name"`
	Items []DashboardItem `json:"items"`
}

// DashboardOut is the miniapp.dashboard.lanes response.
//
//deneb:wire
type DashboardOut struct {
	Lanes []LaneOut `json:"lanes"`
}
