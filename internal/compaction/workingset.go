package compaction

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// WorkingSetFile is one snapshotted file in the working set.
type WorkingSetFile struct {
	Path      string
	Bytes     int64
	Lines     int
	Content   string
	Truncated bool
	LastOp    string
	LastTurn  int
	LastSeq   int
}

// WorkingSetSnapshot is the post-compaction working-set snapshot.
type WorkingSetSnapshot struct {
	CapturedAt string
	Files      []WorkingSetFile
	Skipped    []WorkingSetSkip
	TotalChars int
}

// WorkingSetSkip records why a candidate was not snapshotted.
type WorkingSetSkip struct {
	Path   string
	Reason string
}

// WorkingSetInput configures the working-set collection.
type WorkingSetInput struct {
	Files           []LedgerFile
	Cwd             string
	MaxFiles        int
	MaxCharsPerFile int
	MaxTotalChars   int
	ReadFile        func(path string) ([]byte, error)
	Now             func() string
}

const workingSetMaxFileBytes = 2 * 1024 * 1024

var (
	alwaysSecretRe = regexp.MustCompile(`(^|[\\/])(shiftup-auth\.json|\.env(\..*)?|\.npmrc|\.netrc|\.pypirc|id_rsa[^\\/]*|id_ed25519[^\\/]*|[^\\/]*\.(pem|key|p12|pfx|jks|kdbx))$`)
	secretWordRe   = regexp.MustCompile(`(secret|credential|token|password|apikey|api_key)`)
	dataFileRe     = regexp.MustCompile(`\.(json|ya?ml|toml|ini|cfg|conf|txt|env|properties)$`)
)

// IsSecretLikePath reports whether a path looks like a credential file that
// must never be echoed into a summary.
func IsSecretLikePath(path string) bool {
	if alwaysSecretRe.MatchString(path) {
		return true
	}
	base := filepath.Base(path)
	if !secretWordRe.MatchString(base) {
		return false
	}
	return dataFileRe.MatchString(base) || !strings.Contains(base, ".")
}

func withinCwd(cwd, resolved string) bool {
	rel, err := filepath.Rel(cwd, resolved)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func looksBinary(content string) bool {
	return strings.Contains(content, "\x00")
}

// CollectWorkingSet picks the most recently modified files (newest first) and
// reads them under a budget. Only paths inside the working directory are read;
// secret-looking names are skipped.
func CollectWorkingSet(in WorkingSetInput) WorkingSetSnapshot {
	if in.ReadFile == nil {
		in.ReadFile = func(path string) ([]byte, error) { return os.ReadFile(path) }
	}
	if in.Now == nil {
		in.Now = func() string { return "now" }
	}
	candidates := make([]LedgerFile, 0, len(in.Files))
	for _, f := range in.Files {
		if f.Edits > 0 || f.Writes > 0 {
			candidates = append(candidates, f)
		}
	}
	// Sort newest-first by last seq then last turn.
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && (candidates[j-1].LastSeq < candidates[j].LastSeq || (candidates[j-1].LastSeq == candidates[j].LastSeq && candidates[j-1].LastTurn < candidates[j].LastTurn)); j-- {
			candidates[j-1], candidates[j] = candidates[j], candidates[j-1]
		}
	}
	snap := WorkingSetSnapshot{CapturedAt: in.Now()}
	totalChars := 0
	for _, candidate := range candidates {
		if len(snap.Files) >= in.MaxFiles || in.MaxFiles <= 0 {
			break
		}
		resolved := filepath.Join(in.Cwd, candidate.Path)
		if !withinCwd(in.Cwd, resolved) {
			snap.Skipped = append(snap.Skipped, WorkingSetSkip{Path: candidate.Path, Reason: "outside working directory"})
			continue
		}
		if IsSecretLikePath(candidate.Path) {
			snap.Skipped = append(snap.Skipped, WorkingSetSkip{Path: candidate.Path, Reason: "secret-like file name"})
			continue
		}
		data, err := in.ReadFile(resolved)
		if err != nil {
			snap.Skipped = append(snap.Skipped, WorkingSetSkip{Path: candidate.Path, Reason: "unreadable or missing"})
			continue
		}
		if len(data) > workingSetMaxFileBytes {
			snap.Skipped = append(snap.Skipped, WorkingSetSkip{Path: candidate.Path, Reason: "larger than 2 MB"})
			continue
		}
		content := string(data)
		if looksBinary(content) {
			snap.Skipped = append(snap.Skipped, WorkingSetSkip{Path: candidate.Path, Reason: "binary content"})
			continue
		}
		remaining := in.MaxTotalChars - totalChars
		if remaining < 500 {
			snap.Skipped = append(snap.Skipped, WorkingSetSkip{Path: candidate.Path, Reason: "working-set budget exhausted"})
			continue
		}
		cap := in.MaxCharsPerFile
		if cap > remaining {
			cap = remaining
		}
		cut, trunc, _ := TruncateHeadTail(content, cap, 0.65, func(omitted int) string {
			return fmt.Sprintf("[… %s characters omitted; read the file for the full content]", formatCount(omitted))
		})
		snap.Files = append(snap.Files, WorkingSetFile{
			Path:      candidate.Path,
			Bytes:     int64(len(data)),
			Lines:     strings.Count(content, "\n") + 1,
			Content:   cut,
			Truncated: trunc,
			LastOp:    candidate.LastOp,
			LastTurn:  candidate.LastTurn,
			LastSeq:   candidate.LastSeq,
		})
		totalChars += len(cut)
	}
	snap.TotalChars = totalChars
	return snap
}

// RenderWorkingSet renders the snapshot as Markdown.
func RenderWorkingSet(snap WorkingSetSnapshot) string {
	if len(snap.Files) == 0 && len(snap.Skipped) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("## Working Set Snapshot (as of compaction %s; re-read before editing)", snap.CapturedAt))
	for _, f := range snap.Files {
		fence := "```"
		for strings.Contains(f.Content, fence) {
			fence += "`"
		}
		lines = append(lines, fmt.Sprintf("### %s — %d lines, %d bytes; last %s in T%d", f.Path, f.Lines, f.Bytes, f.LastOp, f.LastTurn))
		if f.Truncated {
			lines = append(lines, "; shown truncated")
		}
		lines = append(lines, fmt.Sprintf("%s\n%s\n%s", fence, f.Content, fence))
	}
	if len(snap.Skipped) > 0 {
		var parts []string
		for _, s := range snap.Skipped {
			parts = append(parts, fmt.Sprintf("%s (%s)", s.Path, s.Reason))
		}
		lines = append(lines, "Not snapshotted: "+strings.Join(parts, "; ")+".")
	}
	return strings.Join(lines, "\n")
}
