package agent

// Scope is descriptive coverage context, never an execution authorization.
func (t *ToolSet) localCoverageScope() []map[string]any {
	out := []map[string]any{}
	rows, err := t.as.ListTaskScope(t.taskID)
	if err != nil {
		return out
	}
	for _, row := range rows {
		entry := map[string]any{"kind": row.Kind, "source": row.Source, "value": row.Value, "domain": row.Domain, "net": row.Net}
		if row.CompanyID != nil && t.cs != nil {
			entry["company_id"] = *row.CompanyID
			if rules, err := t.cs.GetScope(*row.CompanyID); err == nil {
				rendered := make([]map[string]any, 0, len(rules))
				keywords := []string{}
				for _, rule := range rules {
					value := rule.Value
					if rule.Domain != "" {
						value = rule.Domain
					}
					if rule.Net != "" {
						value = rule.Net
					}
					if rule.Raw != "" {
						value = rule.Raw
					}
					rendered = append(rendered, map[string]any{"kind": rule.Kind, "value": value})
					if rule.Kind == "keyword" {
						keywords = append(keywords, value)
					}
				}
				entry["company_scope"] = rendered
				entry["company_keywords"] = keywords
			}
		}
		out = append(out, entry)
	}
	return out
}
