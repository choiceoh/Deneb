package denebui

// convertLayoutElem converts elements whose primary responsibility is arranging
// other UI nodes. The boolean reports whether this family recognizes the tag.
func convertLayoutElem(el *openElem, node map[string]any) (any, bool) {
	a := el.attrs
	switch el.tag {
	case "column", "col":
		node["type"] = "column"
		putNodeID(node, a)
		node["children"] = el.children
	case "row":
		node["type"] = "row"
		putNodeID(node, a)
		node["children"] = el.children
	case "card":
		node["type"] = "card"
		putNodeID(node, a)
		// dismissible: the renderer shows a close affordance and the user can fold
		// the card away ("넘김") without answering it — a decision card the user
		// wants out of the way for now, not a decision made.
		putBool(node, "dismissible", a, "dismissible")
		node["children"] = el.children
	case "box":
		node["type"] = "box"
		putNodeID(node, a)
		node["children"] = el.children
		putStr(node, "contentAlignment", a["align"])
	case "hr", "divider", "spacer":
		// spacer: an invented-but-frequent alias (2026-07-18 reject telemetry) —
		// vertical breathing room reads closest to a divider.
		node["type"] = "divider"
		putNodeID(node, a)
	default:
		return nil, false
	}
	return node, true
}
