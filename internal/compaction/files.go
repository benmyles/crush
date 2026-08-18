package compaction

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// FileRef is a large-file reference with an exploration summary.
type FileRef struct {
	ID          string
	Path        string
	Mime        string
	TokenCount  int64
	Exploration string
}

// ExplorationSummary produces a type-aware exploration summary for a file so the
// model retains awareness of a large file without loading its raw content. The
// dispatcher selects a strategy by file extension/MIME: structured formats
// (JSON/CSV/SQL) get schema/shape extraction; code gets a structural sketch
// (signatures/hierarchies via a best-effort parse); other text gets a short
// truncated preview. This is the deterministic, no-LLM fallback; the engine
// may later upgrade code/text to an LLM summary via the completer.
func ExplorationSummary(path string, content []byte, mime string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case ext == ".json":
		return jsonExploration(content)
	case ext == ".csv":
		return csvExploration(content)
	case ext == ".sql" || ext == ".sqlite" || ext == ".db":
		return "SQL database file: " + path + " (" + fmtBytes(len(content)) + ")"
	case isCodeExt(ext):
		return codeExploration(path, content)
	case isTextExt(ext) || mime == "" || strings.HasPrefix(mime, "text/"):
		return textPreview(content)
	default:
		return fmt.Sprintf("Binary file %s (%s, %s)", path, mime, fmtBytes(len(content)))
	}
}

func jsonExploration(content []byte) string {
	var v any
	if err := json.Unmarshal(content, &v); err != nil {
		return "JSON file (invalid/unparseable): " + err.Error()
	}
	switch root := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(root))
		for k := range root {
			keys = append(keys, k)
		}
		return "JSON object with keys: " + strings.Join(keys, ", ")
	case []any:
		var sb strings.Builder
		fmt.Fprintf(&sb, "JSON array with %d items", len(root))
		if len(root) > 0 {
			if first, ok := root[0].(map[string]any); ok {
				keys := make([]string, 0, len(first))
				for k := range first {
					keys = append(keys, k)
				}
				sb.WriteString("; first item keys: " + strings.Join(keys, ", "))
			}
		}
		return sb.String()
	default:
		return fmt.Sprintf("JSON %T value", root)
	}
}

func csvExploration(content []byte) string {
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	n := len(lines)
	if n == 0 {
		return "CSV file (empty)"
	}
	header := lines[0]
	preview := ""
	if n > 1 {
		preview = "; first data row: " + truncateLine(lines[1], 120)
	}
	return fmt.Sprintf("CSV file: %d rows; header: %s%s", n, truncateLine(header, 200), preview)
}

func codeExploration(path string, content []byte) string {
	text := string(content)
	lines := strings.Split(text, "\n")
	var sigs []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Best-effort signature extraction for common languages.
		if strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "function ") ||
			strings.HasPrefix(trimmed, "def ") || strings.HasPrefix(trimmed, "class ") ||
			strings.HasPrefix(trimmed, "type ") || strings.HasPrefix(trimmed, "interface ") ||
			strings.HasPrefix(trimmed, "struct ") || strings.HasPrefix(trimmed, "pub fn ") ||
			strings.HasPrefix(trimmed, "fn ") {
			sigs = append(sigs, truncateLine(trimmed, 120))
			if len(sigs) >= 20 {
				break
			}
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Code file %s: %d lines", path, len(lines))
	if len(sigs) > 0 {
		sb.WriteString("; top signatures:\n")
		for _, s := range sigs {
			sb.WriteString("  " + s + "\n")
		}
	}
	return sb.String()
}

func textPreview(content []byte) string {
	text := string(content)
	lines := strings.Split(text, "\n")
	preview := lines[0]
	if len(lines) > 1 {
		preview += "\n…"
	}
	preview = truncateLine(preview, 400)
	return fmt.Sprintf("Text file (%d lines, %s). Preview:\n%s", len(lines), fmtBytes(len(content)), preview)
}

func isCodeExt(ext string) bool {
	switch ext {
	case ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".rs", ".java", ".kt", ".rb",
		".c", ".h", ".cpp", ".cc", ".hpp", ".cs", ".swift", ".scala", ".clj",
		".ex", ".exs", ".erl", ".hs", ".ml", ".fs", ".fsx", ".lua", ".php",
		".pl", ".r", ".sh", ".bash", ".zsh", ".fish", ".vim", ".el", ".lisp":
		return true
	}
	return false
}

func isTextExt(ext string) bool {
	switch ext {
	case ".txt", ".md", ".rst", ".log", ".yaml", ".yml", ".toml", ".ini",
		".cfg", ".conf", ".properties", ".xml", ".html", ".css", ".scss":
		return true
	}
	return false
}

func truncateLine(s string, max int) string {
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

func fmtBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
