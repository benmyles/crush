// Command tmp-patch-cassettes updates the recorded coder system prompt in
// all VCR cassettes under internal/agent/testdata to include the
// <terminal_title> block added to coder.md.tpl. It is an offline
// maintenance utility: replay must match the request bytes the agent now
// sends, and re-recording requires a live provider key.
//
// The patch is a pure textual splice on the raw JSON request body, so
// every byte outside the system prompt (JSON key order, escaping) is
// preserved exactly. Body strings stored in cassettes are raw JSON text,
// so all newlines in the insert are literal backslash-n sequences.
//
// Usage: go run ./tmp-patch-cassettes
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
)

// The pre-terminal_title prompt ends its skills section with five
// backslash-n newlines before the system message closes. The splice lands
// one more newline, the terminal_title block, and a trailing newline in
// between, leaving the `","role":"system"` tail untouched.
const (
	skillsTail = `</skills_usage>\n\n\n\n\n`
	insert     = `\n<terminal_title>\nKeep the terminal window title in sync with your current work using the\n` +
		"`set_terminal_title` tool. The title is shown in the user's terminal" + `\n` +
		"tab or window title bar next to a small state glyph." + `\n\n` +
		"- Set it whenever you start a new task, meaningfully change what you" + `\n` +
		"  are working on, or your work completes." + `\n` +
		"- Curate a terse 2-4 word phrase in lowercase, present tense, e.g." + `\n` +
		`  \"fixing deploy pipeline\", \"migrating auth queries\", or \"writing` + `\n` +
		`  database tests\".` + `\n` +
		"- Clear it (empty `title`) once the task is finished, so the default" + `\n` +
		"  prompt-based title returns." + `\n` +
		"- Never include secrets, tokens, or sensitive values in titles." + `\n</terminal_title>` + `\n`
)

// The terminal-tool guidance was appended to the tool_usage section right
// before the bash_commands block. The splice inserts the two new bullets
// between the last tool_usage bullet and the section break.
const (
	toolUsageTail   = "- Only use the tools you know exist.\\n\\n<bash_commands>"
	toolUsageInsert = "- Only use the tools you know exist.\\n\\n" +
		"- Prefer the terminal tools (terminal_start, terminal_input, terminal_output, terminal_kill) over the bash tool for interactive sessions and long-running commands; use bash for quick non-interactive commands.\\n" +
		"- When waiting on terminal_output, never wait_for text that also appears in the command you typed: the terminal echoes the command first and matches immediately. Compute a completion marker instead, e.g. `echo DONE_$((6*7))` with wait_for \\\"DONE_42\\\".\\n\\n<bash_commands>"
)

func main() {
	var files []string
	err := filepath.WalkDir("internal/agent/testdata", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".yaml") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

	total := 0
	for _, file := range files {
		name := strings.TrimSuffix(file, ".yaml")
		c, err := cassette.Load(name)
		if err != nil {
			panic(fmt.Errorf("%s: %w", file, err))
		}
		// Load does not restore the marshaler; replay tests record with
		// the same yaml encoder settings as below, so reuse them to keep
		// the file layout byte-stable.
		c.MarshalFunc = func(in any) ([]byte, error) {
			var buff bytes.Buffer
			enc := yaml.NewEncoder(&buff)
			enc.SetIndent(2)
			enc.CompactSeqIndent()
			if err := enc.Encode(in); err != nil {
				return nil, err
			}
			return buff.Bytes(), nil
		}
		changed := 0
		for i := range c.Interactions {
			body := c.Interactions[i].Request.Body
			if body == "" {
				continue
			}
			patched := false
			if strings.Contains(body, skillsTail) && !strings.Contains(body, "<terminal_title>") {
				body = strings.ReplaceAll(body, skillsTail, skillsTail+insert)
				patched = true
			}
			if strings.Contains(body, toolUsageTail) && !strings.Contains(body, "Compute a completion marker instead") {
				body = strings.ReplaceAll(body, toolUsageTail, toolUsageInsert)
				patched = true
			}
			if patched {
				c.Interactions[i].Request.Body = body
				changed++
			}
		}
		if changed > 0 {
			if err := c.Save(); err != nil {
				panic(fmt.Errorf("%s: %w", file, err))
			}
			fmt.Printf("patched %s: %d request(s)\n", file, changed)
			total += changed
		}
	}
	fmt.Printf("done: %d requests patched\n", total)
}
