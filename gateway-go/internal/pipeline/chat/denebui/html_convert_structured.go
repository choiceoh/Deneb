package denebui

// convertStructuredElem dispatches parser-only child records to the family
// that owns their parent/child contract. Each family converts both the rendered
// parent node and the structural intermediates that only that parent consumes.
func convertStructuredElem(el *openElem, inner string, node map[string]any) (any, bool) {
	switch el.tag {
	case "chart", "point":
		return convertChartElement(el, node), true
	case "table", "tr", "td", "th":
		return convertTableElement(el, inner, node), true
	case "ul", "ol", "list", "li":
		return convertListElement(el, inner, node), true
	case "tabs", "tab":
		return convertTabsElement(el, node), true
	case "accordion":
		return convertAccordionElement(el, node), true
	default:
		return nil, false
	}
}

func convertChartElement(el *openElem, node map[string]any) any {
	if el.tag == "point" {
		return structural{kind: "point", attrs: el.attrs}
	}
	node["type"] = "chart"
	putNodeID(node, el.attrs)
	putStr(node, "chartType", canonChartType(el.attrs["type"]))
	putStr(node, "label", el.attrs["label"])
	labels, values := chartPointSeries(el.structs)
	node["labels"], node["values"] = labels, values
	return node
}

// chartPointSeries accepts only point intermediates. A malformed numeric value
// retains the parser's historical zero fallback so labels and values stay
// positionally aligned for renderers.
func chartPointSeries(structs []any) ([]any, []any) {
	labels, values := []any{}, []any{}
	for _, value := range structs {
		point, ok := value.(structural)
		if !ok || point.kind != "point" {
			continue
		}
		labels = append(labels, point.attrs["label"])
		number, _ := lenientFloat(point.attrs["value"])
		values = append(values, number)
	}
	return labels, values
}

func convertTableElement(el *openElem, inner string, node map[string]any) any {
	switch el.tag {
	case "tr":
		return structural{kind: "tr", structs: el.structs}
	case "td", "th":
		header := "false"
		if el.tag == "th" {
			header = "true"
		}
		return structural{
			kind:  "cell",
			text:  inner,
			attrs: map[string]string{"__th": header},
		}
	}
	node["type"] = "table"
	putNodeID(node, el.attrs)
	headers, rows := tableRows(el.structs)
	node["headers"], node["rows"] = headers, rows
	return node
}

func tableRows(structs []any) ([]any, []any) {
	headers, rows := []any{}, []any{}
	for _, value := range structs {
		row, ok := value.(structural)
		if !ok || row.kind != "tr" {
			continue
		}
		cells, headerBearing := tableCells(row.structs)
		if headerBearing && len(headers) == 0 {
			headers = cells
			continue
		}
		rows = append(rows, cells)
	}
	return headers, rows
}

func tableCells(structs []any) ([]any, bool) {
	var cells []any
	headerBearing := false
	for _, value := range structs {
		cell, ok := value.(structural)
		if !ok || cell.kind != "cell" {
			continue
		}
		headerBearing = headerBearing || cell.attrs["__th"] == "true"
		cells = append(cells, cell.text)
	}
	return cells, headerBearing
}

func convertListElement(el *openElem, inner string, node map[string]any) any {
	if el.tag == "li" {
		return structural{kind: "li", text: inner, children: el.children}
	}
	node["type"] = "list"
	putNodeID(node, el.attrs)
	if _, present := el.attrs["ordered"]; el.tag == "ol" || (present && truthy(el.attrs["ordered"])) {
		node["ordered"] = true
	}
	node["items"] = listItems(el.structs)
	return node
}

func listItems(structs []any) []any {
	items := []any{}
	for _, value := range structs {
		item, ok := value.(structural)
		if !ok || item.kind != "li" {
			continue
		}
		items = append(items, renderListItem(item))
	}
	return items
}

// renderListItem preserves the compact renderer contract: bare text becomes a
// text node, one child stays unwrapped, and multiple children gain a column.
func renderListItem(item structural) any {
	switch len(item.children) {
	case 0:
		return map[string]any{"type": "text", "value": item.text}
	case 1:
		return item.children[0]
	default:
		return map[string]any{"type": "column", "children": item.children}
	}
}

func convertTabsElement(el *openElem, node map[string]any) any {
	if el.tag == "tab" {
		return structural{kind: "tab", attrs: el.attrs, children: el.children}
	}
	node["type"] = "tabs"
	putNodeID(node, el.attrs)
	putNum(node, "selectedIndex", el.attrs["selected-index"], true)
	node["tabs"] = tabDefinitions(el.structs)
	return node
}

func tabDefinitions(structs []any) []any {
	tabs := []any{}
	for _, value := range structs {
		tab, ok := value.(structural)
		if !ok || tab.kind != "tab" {
			continue
		}
		tabs = append(tabs, map[string]any{"label": tab.attrs["label"], "children": tab.children})
	}
	return tabs
}

func convertAccordionElement(el *openElem, node map[string]any) any {
	node["type"] = "accordion"
	putNodeID(node, el.attrs)
	node["title"] = el.attrs["title"]
	node["children"] = el.children
	putBool(node, "expanded", el.attrs, "expanded")
	return node
}
