package denebui

import "strings"

// convertFormElem converts interactive controls and their parser-only option
// records.
func convertFormElem(el *openElem, inner string, node map[string]any) (any, bool) {
	a := el.attrs
	switch el.tag {
	case "button":
		node["type"] = "button"
		putNodeID(node, a)
		node["label"] = firstNonEmpty(a["label"], inner)
		putStr(node, "variant", a["variant"])
		putBool(node, "enabled", a, "enabled")
		if act := actionFromAttrs(a); act != nil {
			node["action"] = act
		}
	case "input":
		switch strings.ToLower(a["type"]) {
		case "date":
			node["type"] = "date_input"
			fillInput(node, a, false)
		case "time":
			node["type"] = "time_input"
			fillInput(node, a, false)
		case "checkbox":
			node["type"] = "checkbox"
			node["id"] = a["id"]
			node["label"] = a["label"]
			putBool(node, "checked", a, "checked")
		default:
			node["type"] = "text_input"
			fillInput(node, a, true)
		}
	case "textarea":
		node["type"] = "text_input"
		fillInput(node, a, true)
		node["multiline"] = true
		if inner != "" {
			node["value"] = inner
		}
	case "checkbox":
		node["type"] = "checkbox"
		node["id"] = a["id"]
		node["label"] = firstNonEmpty(a["label"], inner)
		putBool(node, "checked", a, "checked")
	case "switch":
		node["type"] = "switch"
		node["id"] = a["id"]
		node["label"] = firstNonEmpty(a["label"], inner)
		putBool(node, "checked", a, "checked")
	case "select", "radio-group", "radiogroup":
		if el.tag == "select" {
			node["type"] = "select"
			putStr(node, "placeholder", a["placeholder"])
		} else {
			node["type"] = "radio_group"
		}
		node["id"] = a["id"]
		putStr(node, "label", a["label"])
		putBool(node, "required", a, "required")
		opts := []any{}
		for _, s := range el.structs {
			op, ok := s.(structural)
			if !ok || op.kind != "option" {
				continue
			}
			opts = append(opts, op.text)
			if v, present := op.attrs["selected"]; present && truthy(v) {
				node["selected"] = op.text
			}
		}
		node["options"] = opts
		if v := a["selected"]; v != "" {
			node["selected"] = v
		}
	case "option":
		return structural{kind: "option", text: inner, attrs: a}, true
	case "slider":
		node["type"] = "slider"
		node["id"] = a["id"]
		putStr(node, "label", a["label"])
		putNum(node, "value", a["value"], false)
		putNum(node, "min", a["min"], false)
		putNum(node, "max", a["max"], false)
		putNum(node, "step", a["step"], false)
	case "chips", "chip-group":
		node["type"] = "chip_group"
		node["id"] = a["id"]
		if v := a["selection"]; v != "" {
			node["selection"] = v
		}
		putBool(node, "required", a, "required")
		// layout="list" turns the flow of chips into a vertical choice list
		// (selection control + optional letter badge + label + description) —
		// the shape a "pick one or more of these" question wants. Any other
		// value, invented ones included, keeps the default flow: a cosmetic
		// attribute must never be what invalidates a card.
		if strings.EqualFold(strings.TrimSpace(a["layout"]), "list") {
			node["layout"] = "list"
		}
		putBool(node, "lettered", a, "lettered")
		chips := []any{}
		for _, s := range el.structs {
			ch, ok := s.(structural)
			if !ok || ch.kind != "chip" {
				continue
			}
			val := firstNonEmpty(ch.attrs["value"], ch.text)
			chip := map[string]any{"label": ch.text, "value": val}
			// One-line description under the label ("GitHub — 레포, 이슈, PR").
			if d := strings.TrimSpace(ch.attrs["description"]); d != "" {
				chip["description"] = d
			}
			chips = append(chips, chip)
		}
		node["chips"] = chips
	case "chip":
		return structural{kind: "chip", text: inner, attrs: a}, true
	case "br":
		return nil, true
	default:
		return nil, false
	}
	return node, true
}
