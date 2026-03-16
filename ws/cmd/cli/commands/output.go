package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
)

// printOutput renders data in the requested format.
func printOutput(data any, format string) error {
	switch format {
	case "json":
		return printJSON(data)
	default:
		return printTable(data)
	}
}

func printJSON(data any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return nil
}

func printTable(data any) error {
	m, ok := data.(map[string]any)
	if !ok {
		return printJSON(data)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Handle lists
	if tenants, ok := m["tenants"].([]any); ok {
		_, _ = fmt.Fprintln(w, "ID\tNAME\tSTATUS\tCONSUMER_TYPE")
		for _, t := range tenants {
			tm := asMap(t)
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				asStr(tm, "id"), asStr(tm, "name"), asStr(tm, "status"), asStr(tm, "consumer_type"))
		}
		_ = w.Flush()
		if total, ok := m["total"]; ok {
			_, _ = fmt.Fprintf(os.Stdout, "\nTotal: %v\n", total)
		}
		return nil
	}

	if keys, ok := m["keys"].([]any); ok {
		_, _ = fmt.Fprintln(w, "KEY_ID\tTENANT_ID\tALGORITHM\tACTIVE")
		for _, k := range keys {
			km := asMap(k)
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%v\n",
				asStr(km, "key_id"), asStr(km, "tenant_id"), asStr(km, "algorithm"), km["is_active"])
		}
		_ = w.Flush()
		return nil
	}

	if topics, ok := m["topics"].([]any); ok {
		_, _ = fmt.Fprintln(w, "TOPIC")
		for _, t := range topics {
			_, _ = fmt.Fprintf(w, "%v\n", t)
		}
		_ = w.Flush()
		return nil
	}

	if entries, ok := m["entries"].([]any); ok {
		_, _ = fmt.Fprintln(w, "ACTION\tTENANT_ID\tACTOR\tCREATED_AT")
		for _, e := range entries {
			em := asMap(e)
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				asStr(em, "action"), asStr(em, "tenant_id"), asStr(em, "actor"), asStr(em, "created_at"))
		}
		_ = w.Flush()
		return nil
	}

	// Single resource or status response — print key-value pairs
	for k, v := range m {
		_, _ = fmt.Fprintf(w, "%s\t%v\n", k, v)
	}
	_ = w.Flush()
	return nil
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}
