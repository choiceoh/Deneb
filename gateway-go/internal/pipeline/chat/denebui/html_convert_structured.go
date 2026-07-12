package denebui

// convertStructuredElem converts nodes built from parser-only structural
// children, such as table rows, chart points, and tab definitions.
func convertStructuredElem(el *openElem, inner string, node map[string]any) (any, bool) {
	a := el.attrs
	switch el.tag {
	case "chart":
		node["type"] = "chart"
		putNodeID(node, a)
		putStr(node, "chartType", canonChartType(a["type"]))
		putStr(node, "label", a["label"])
		labels, values := []any{}, []any{}
		for _, s := range el.structs {
			pt, ok := s.(structural)
			if !ok || pt.kind != "point" {
				continue
			}
			labels = append(labels, pt.attrs["label"])
			f, _ := lenientFloat(pt.attrs["value"])
			values = append(values, f)
		}
		node["labels"], node["values"] = labels, values
	case "point":
		return structural{kind: "point", attrs: a}, true
	case "table":
		node["type"] = "table"
		putNodeID(node, a)
		headers, rows := []any{}, []any{}
		for _, s := range el.structs {
			tr, ok := s.(structural)
			if !ok || tr.kind != "tr" {
				continue
			}
			var cells []any
			hasTH := false
			for _, cs := range tr.structs {
				c, cok := cs.(structural)
				if !cok || c.kind != "cell" {
					continue
				}
				if c.attrs["__th"] == "true" {
					hasTH = true
				}
				cells = append(cells, c.text)
			}
			if hasTH && len(headers) == 0 {
				headers = cells
			} else {
				rows = append(rows, cells)
			}
		}
		node["headers"], node["rows"] = headers, rows
	case "tr":
		return structural{kind: "tr", structs: el.structs}, true
	case "td", "th":
		th := "false"
		if el.tag == "th" {
			th = "true"
		}
		return structural{kind: "cell", text: inner, attrs: map[string]string{"__th": th}}, true
	case "ul", "ol", "list":
		node["type"] = "list"
		putNodeID(node, a)
		ordered := el.tag == "ol" || truthy(a["ordered"])
		if ordered {
			node["ordered"] = true
		}
		items := []any{}
		for _, s := range el.structs {
			li, ok := s.(structural)
			if !ok || li.kind != "li" {
				continue
			}
			switch len(li.children) {
			case 0:
				items = append(items, map[string]any{"type": "text", "value": li.text})
			case 1:
				items = append(items, li.children[0])
			default:
				items = append(items, map[string]any{"type": "column", "children": li.children})
			}
		}
		node["items"] = items
	case "li":
		return structural{kind: "li", text: inner, children: el.children}, true
	case "tabs":
		node["type"] = "tabs"
		putNodeID(node, a)
		putNum(node, "selectedIndex", a["selected-index"], true)
		tabs := []any{}
		for _, s := range el.structs {
			tb, ok := s.(structural)
			if !ok || tb.kind != "tab" {
				continue
			}
			tabs = append(tabs, map[string]any{"label": tb.attrs["label"], "children": tb.children})
		}
		node["tabs"] = tabs
	case "tab":
		return structural{kind: "tab", attrs: a, children: el.children}, true
	case "accordion":
		node["type"] = "accordion"
		putNodeID(node, a)
		node["title"] = a["title"]
		node["children"] = el.children
		putBool(node, "expanded", a, "expanded")
	default:
		return nil, false
	}
	return node, true
}
