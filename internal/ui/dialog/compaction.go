package dialog

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/ui/util"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
)

const (
	// CompactionSettingsID is the identifier for the compaction settings dialog.
	CompactionSettingsID              = "compaction_settings"
	compactionSettingsDialogMaxWidth  = 72
	compactionSettingsDialogMinHeight = 10
	compactionSettingsDialogMaxHeight = 26
)

// compactionSettingKind is how a setting row is edited.
type compactionSettingKind int

const (
	// settingKindBool toggles on enter.
	settingKindBool compactionSettingKind = iota
	// settingKindEnum cycles through Choices on enter.
	settingKindEnum
	// settingKindInt opens the inline editor for a non-negative integer.
	settingKindInt
	// settingKindFraction opens the inline editor for a number in (0, 1].
	settingKindFraction
	// settingKindFloat opens the inline editor for any number.
	settingKindFloat
	// settingKindModel opens the model picker on the compaction slot.
	settingKindModel
	// settingKindReset resets every compaction option to its default.
	settingKindReset
	// settingKindModelReasoning cycles the compaction model's reasoning effort.
	settingKindModelReasoning
	// settingKindModelThinking toggles the compaction model's thinking mode.
	settingKindModelThinking
)

// compactionSettingSpec describes one editable compaction option.
type compactionSettingSpec struct {
	// Key is the JSON key under options.compaction (empty for rows that do
	// not map onto an option, e.g. the model rows and reset).
	Key     string
	Label   string
	Kind    compactionSettingKind
	Choices []string
	// MinExclusive requires ints to be > 0 (instead of >= 0).
	MinExclusive bool
	// Help is a short explanation shown after the value.
	Help string
}

// compactionSettingSpecs is the ordered list of settings shown by the dialog
// (model rows are inserted at the top at render time).
var compactionSettingSpecs = []compactionSettingSpec{
	{Key: "enabled", Label: "Compaction engine", Kind: settingKindBool, Help: "off = legacy"},
	{Key: "reserve_tokens", Label: "Reserve tokens", Kind: settingKindInt, MinExclusive: true, Help: "hard headroom"},
	{Key: "keep_recent_tokens", Label: "Keep recent tokens", Kind: settingKindInt, MinExclusive: true, Help: "kept verbatim"},
	{Key: "soft_threshold_fraction", Label: "Soft threshold", Kind: settingKindFraction, Help: "of window"},
	{Key: "budget_fraction", Label: "Budget fraction", Kind: settingKindFraction, Help: "of window"},
	{Key: "max_summary_tokens", Label: "Max summary tokens", Kind: settingKindInt, MinExclusive: true},
	{Key: "min_summary_tokens", Label: "Min summary tokens", Kind: settingKindInt, MinExclusive: true},
	{Key: "verify", Label: "Coverage audit", Kind: settingKindEnum, Choices: []string{string(config.VerificationJudge), string(config.VerificationChecks), string(config.VerificationOff)}, Help: "audit mode"},
	{Key: "ledger", Label: "Deterministic ledger", Kind: settingKindBool},
	{Key: "transcript_map", Label: "Transcript map", Kind: settingKindBool},
	{Key: "working_set_files", Label: "Working-set files", Kind: settingKindInt, Help: "0 disables"},
	{Key: "working_set_max_chars_per_file", Label: "Working-set chars/file", Kind: settingKindInt, MinExclusive: true},
	{Key: "extracts_decay", Label: "Extracts decay", Kind: settingKindFloat, Help: "0 = older off"},
	{Key: "parallel_block_threshold", Label: "Parallel block threshold", Kind: settingKindInt, Help: "tokens, 0 = off"},
}

// CompactionSettingItem is one row of the compaction settings dialog.
type CompactionSettingItem struct {
	*list.Versioned
	spec      compactionSettingSpec
	value     string
	isDefault bool
	t         *styles.Styles
	m         fuzzy.Match
	cache     map[int]string
	focused   bool
}

var (
	_ Dialog   = (*CompactionSettings)(nil)
	_ ListItem = (*CompactionSettingItem)(nil)
)

// Finished implements list.Item.
func (i *CompactionSettingItem) Finished() bool { return true }

// Filter implements ListItem.
func (i *CompactionSettingItem) Filter() string { return i.spec.Label }

// ID implements ListItem.
func (i *CompactionSettingItem) ID() string {
	if i.spec.Key != "" {
		return i.spec.Key
	}
	return fmt.Sprintf("kind-%d", i.spec.Kind)
}

// SetFocused implements ListItem.
func (i *CompactionSettingItem) SetFocused(focused bool) {
	if i.focused == focused {
		return
	}
	i.cache = nil
	i.focused = focused
	if i.Versioned != nil {
		i.Bump()
	}
}

// SetMatch implements ListItem.
func (i *CompactionSettingItem) SetMatch(m fuzzy.Match) {
	if sameFuzzyMatch(i.m, m) {
		return
	}
	i.cache = nil
	i.m = m
	if i.Versioned != nil {
		i.Bump()
	}
}

// Render implements ListItem.
func (i *CompactionSettingItem) Render(width int) string {
	info := i.value
	if i.isDefault && i.value != "" {
		info += " (default)"
	}
	if i.spec.Help != "" {
		if info != "" {
			info += " · "
		}
		info += i.spec.Help
	}
	styles := ListItemStyles{
		ItemBlurred:     i.t.Dialog.NormalItem,
		ItemFocused:     i.t.Dialog.SelectedItem,
		InfoTextBlurred: i.t.Dialog.ListItem.InfoBlurred,
		InfoTextFocused: i.t.Dialog.ListItem.InfoFocused,
	}
	return renderItem(styles, i.spec.Label, info, i.focused, width, i.cache, &i.m)
}

// CompactionSettings is the dialog that edits the context compaction engine
// settings (options.compaction) and the compaction model slot from the TUI.
type CompactionSettings struct {
	com   *common.Common
	help  help.Model
	list  *list.FilterableList
	input textinput.Model

	// editing is the row currently being edited in the input, or nil when
	// the input is a filter box.
	editing *CompactionSettingItem

	keyMap struct {
		Select   key.Binding
		Cancel   key.Binding
		Next     key.Binding
		Previous key.Binding
		UpDown   key.Binding
		Close    key.Binding
	}
}

// NewCompactionSettings creates the compaction settings dialog.
func NewCompactionSettings(com *common.Common) *CompactionSettings {
	c := &CompactionSettings{com: com}

	help := help.New()
	help.Styles = com.Styles.DialogHelpStyles()
	c.help = help

	c.list = list.NewFilterableList()
	c.list.Focus()

	c.input = textinput.New()
	c.input.SetVirtualCursor(false)
	c.input.Placeholder = "Type to filter"
	c.input.SetStyles(com.Styles.TextInput)
	c.input.Focus()

	c.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "toggle/edit"),
	)
	c.keyMap.Cancel = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel edit"),
	)
	c.keyMap.Next = key.NewBinding(
		key.WithKeys("down", "ctrl+n"),
		key.WithHelp("↓", "next item"),
	)
	c.keyMap.Previous = key.NewBinding(
		key.WithKeys("up", "ctrl+p"),
		key.WithHelp("↑", "previous item"),
	)
	c.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	c.keyMap.Close = CloseKey

	c.Refresh()
	return c
}

// ID implements Dialog.
func (c *CompactionSettings) ID() string { return CompactionSettingsID }

// Refresh re-reads the configuration and rebuilds the rows, keeping the
// current selection where possible. Call it after a setting was written.
func (c *CompactionSettings) Refresh() {
	cfg := c.com.Config()
	resolved := config.ResolveCompactionConfig(cfg)
	var raw *config.CompactionConfig
	if cfg != nil && cfg.Options != nil {
		raw = cfg.Options.Compaction
	}

	var selectedID string
	if sel, ok := c.list.SelectedItem().(*CompactionSettingItem); ok {
		selectedID = sel.ID()
	}

	items := make([]list.FilterableItem, 0, len(compactionSettingSpecs)+4)
	items = append(items, c.modelItems(cfg)...)
	for _, spec := range compactionSettingSpecs {
		value, isDefault := compactionSettingValue(spec, resolved, raw)
		items = append(items, c.newItem(spec, value, isDefault))
	}
	items = append(items, c.newItem(compactionSettingSpec{Label: "Reset all to defaults", Kind: settingKindReset}, "", false))
	c.list.SetItems(items...)

	selectedIndex := 0
	for i, it := range items {
		if it.(*CompactionSettingItem).ID() == selectedID {
			selectedIndex = i
			break
		}
	}
	c.list.SetSelected(selectedIndex)
	c.list.ScrollToSelected()
}

func (c *CompactionSettings) newItem(spec compactionSettingSpec, value string, isDefault bool) *CompactionSettingItem {
	return &CompactionSettingItem{
		Versioned: list.NewVersioned(),
		spec:      spec,
		value:     value,
		isDefault: isDefault,
		t:         c.com.Styles,
		cache:     make(map[int]string),
	}
}

// modelItems builds the compaction-model rows: the slot itself plus, when a
// dedicated model is selected, its reasoning effort or thinking toggle.
func (c *CompactionSettings) modelItems(cfg *config.Config) []list.FilterableItem {
	var items []list.FilterableItem
	if cfg == nil {
		return items
	}
	sel, configured := cfg.Models[config.SelectedModelTypeCompaction]
	value := "same as large model"
	isDefault := true
	if configured {
		isDefault = false
		value = sel.Provider + "/" + sel.Model
		if model := cfg.GetModel(sel.Provider, sel.Model); model != nil && model.Name != "" {
			value = model.Name
		}
	}
	items = append(items, c.newItem(compactionSettingSpec{Label: "Compaction model", Kind: settingKindModel}, value, isDefault))
	if !configured {
		return items
	}
	model := cfg.GetModel(sel.Provider, sel.Model)
	if model == nil || !model.CanReason {
		return items
	}
	if len(model.ReasoningLevels) > 0 {
		effort := sel.ReasoningEffort
		if effort == "" {
			effort = model.DefaultReasoningEffort
		}
		items = append(items, c.newItem(compactionSettingSpec{Label: "Compaction model reasoning", Kind: settingKindModelReasoning, Choices: model.ReasoningLevels}, common.FormatReasoningEffort(effort), false))
		return items
	}
	thinking := "off"
	if sel.Think {
		thinking = "on"
	}
	items = append(items, c.newItem(compactionSettingSpec{Label: "Compaction model thinking", Kind: settingKindModelThinking}, thinking, false))
	return items
}

// compactionSettingValue renders the effective value of a setting and reports
// whether it comes from the defaults (not explicitly configured).
func compactionSettingValue(spec compactionSettingSpec, resolved config.CompactionConfig, raw *config.CompactionConfig) (value string, isDefault bool) {
	isDefault = raw == nil
	switch spec.Key {
	case "enabled":
		value = onOff(resolved.Enabled != nil && *resolved.Enabled)
		isDefault = isDefault || raw.Enabled == nil
	case "reserve_tokens":
		value = strconv.FormatInt(resolved.ReserveTokens, 10)
		isDefault = isDefault || raw.ReserveTokens == 0
	case "keep_recent_tokens":
		value = strconv.FormatInt(resolved.KeepRecentTokens, 10)
		isDefault = isDefault || raw.KeepRecentTokens == 0
	case "soft_threshold_fraction":
		value = formatFloat(resolved.SoftThresholdFraction)
		isDefault = isDefault || raw.SoftThresholdFraction == 0
	case "budget_fraction":
		value = formatFloat(resolved.BudgetFraction)
		isDefault = isDefault || raw.BudgetFraction == 0
	case "max_summary_tokens":
		value = strconv.FormatInt(resolved.MaxSummaryTokens, 10)
		isDefault = isDefault || raw.MaxSummaryTokens == 0
	case "min_summary_tokens":
		value = strconv.FormatInt(resolved.MinSummaryTokens, 10)
		isDefault = isDefault || raw.MinSummaryTokens == 0
	case "verify":
		value = resolved.Verify
		isDefault = isDefault || raw.Verify == ""
	case "ledger":
		value = onOff(resolved.Ledger != nil && *resolved.Ledger)
		isDefault = isDefault || raw.Ledger == nil
	case "transcript_map":
		value = onOff(resolved.TranscriptMap != nil && *resolved.TranscriptMap)
		isDefault = isDefault || raw.TranscriptMap == nil
	case "working_set_files":
		value = strconv.Itoa(resolved.WorkingSetFiles)
		isDefault = isDefault || (raw.WorkingSetFiles == 0 && raw.WorkingSetMaxCharsPerFile == 0)
	case "working_set_max_chars_per_file":
		value = strconv.Itoa(resolved.WorkingSetMaxCharsPerFile)
		isDefault = isDefault || raw.WorkingSetMaxCharsPerFile == 0
	case "extracts_decay":
		value = formatFloat(resolved.ExtractsDecayValue())
		isDefault = isDefault || raw.ExtractsDecay == nil
	case "parallel_block_threshold":
		if resolved.ParallelBlockThreshold == 0 {
			value = "off"
		} else {
			value = strconv.FormatInt(resolved.ParallelBlockThreshold, 10)
		}
		isDefault = isDefault || raw.ParallelBlockThreshold == 0
	}
	return value, isDefault
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// HandleMsg implements Dialog.
func (c *CompactionSettings) HandleMsg(msg tea.Msg) Action {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	if c.editing != nil {
		switch {
		case key.Matches(keyMsg, c.keyMap.Cancel), key.Matches(keyMsg, c.keyMap.Close):
			c.stopEditing()
			return nil
		case key.Matches(keyMsg, c.keyMap.Select):
			action := c.commitEdit()
			c.stopEditing()
			return action
		default:
			var cmd tea.Cmd
			c.input, cmd = c.input.Update(keyMsg)
			return ActionCmd{cmd}
		}
	}

	switch {
	case key.Matches(keyMsg, c.keyMap.Close):
		return ActionClose{}
	case key.Matches(keyMsg, c.keyMap.Previous):
		c.list.Focus()
		if c.list.IsSelectedFirst() {
			c.list.SelectLast()
			c.list.ScrollToBottom()
			break
		}
		c.list.SelectPrev()
		c.list.ScrollToSelected()
	case key.Matches(keyMsg, c.keyMap.Next):
		c.list.Focus()
		if c.list.IsSelectedLast() {
			c.list.SelectFirst()
			c.list.ScrollToTop()
			break
		}
		c.list.SelectNext()
		c.list.ScrollToSelected()
	case key.Matches(keyMsg, c.keyMap.Select):
		item, ok := c.list.SelectedItem().(*CompactionSettingItem)
		if !ok {
			break
		}
		return c.activate(item)
	default:
		var cmd tea.Cmd
		c.input, cmd = c.input.Update(keyMsg)
		c.list.SetFilter(c.input.Value())
		c.list.ScrollToTop()
		c.list.SetSelected(0)
		return ActionCmd{cmd}
	}
	return nil
}

// activate handles enter on a row: toggle, cycle, open the editor, or emit
// the model/reset actions.
func (c *CompactionSettings) activate(item *CompactionSettingItem) Action {
	cfg := c.com.Config()
	resolved := config.ResolveCompactionConfig(cfg)
	spec := item.spec
	switch spec.Kind {
	case settingKindModel:
		return ActionOpenCompactionModel{}
	case settingKindReset:
		return ActionResetCompactionOptions{}
	case settingKindModelReasoning, settingKindModelThinking:
		sel, ok := cfg.Models[config.SelectedModelTypeCompaction]
		if !ok {
			return nil
		}
		if spec.Kind == settingKindModelThinking {
			sel.Think = !sel.Think
			return ActionUpdateCompactionModel{Model: sel, Message: "Compaction model thinking " + onOff(sel.Think)}
		}
		next := nextChoice(spec.Choices, sel.ReasoningEffort)
		if next == "" {
			return nil
		}
		sel.ReasoningEffort = next
		return ActionUpdateCompactionModel{Model: sel, Message: "Compaction model reasoning effort set to " + next}
	case settingKindBool:
		current := false
		switch spec.Key {
		case "enabled":
			current = resolved.Enabled != nil && *resolved.Enabled
		case "ledger":
			current = resolved.Ledger != nil && *resolved.Ledger
		case "transcript_map":
			current = resolved.TranscriptMap != nil && *resolved.TranscriptMap
		}
		return ActionSetCompactionOption{Key: spec.Key, Value: !current, Message: fmt.Sprintf("%s: %s", spec.Label, onOff(!current))}
	case settingKindEnum:
		next := nextChoice(spec.Choices, resolved.Verify)
		return ActionSetCompactionOption{Key: spec.Key, Value: next, Message: fmt.Sprintf("%s: %s", spec.Label, next)}
	case settingKindInt, settingKindFraction, settingKindFloat:
		c.startEditing(item, currentNumericValue(spec, resolved))
		return nil
	}
	return nil
}

func nextChoice(choices []string, current string) string {
	if len(choices) == 0 {
		return ""
	}
	for i, ch := range choices {
		if ch == current {
			return choices[(i+1)%len(choices)]
		}
	}
	return choices[0]
}

func currentNumericValue(spec compactionSettingSpec, resolved config.CompactionConfig) string {
	switch spec.Key {
	case "reserve_tokens":
		return strconv.FormatInt(resolved.ReserveTokens, 10)
	case "keep_recent_tokens":
		return strconv.FormatInt(resolved.KeepRecentTokens, 10)
	case "soft_threshold_fraction":
		return formatFloat(resolved.SoftThresholdFraction)
	case "budget_fraction":
		return formatFloat(resolved.BudgetFraction)
	case "max_summary_tokens":
		return strconv.FormatInt(resolved.MaxSummaryTokens, 10)
	case "min_summary_tokens":
		return strconv.FormatInt(resolved.MinSummaryTokens, 10)
	case "working_set_files":
		return strconv.Itoa(resolved.WorkingSetFiles)
	case "working_set_max_chars_per_file":
		return strconv.Itoa(resolved.WorkingSetMaxCharsPerFile)
	case "extracts_decay":
		return formatFloat(resolved.ExtractsDecayValue())
	case "parallel_block_threshold":
		return strconv.FormatInt(resolved.ParallelBlockThreshold, 10)
	}
	return ""
}

func (c *CompactionSettings) startEditing(item *CompactionSettingItem, current string) {
	c.editing = item
	c.list.SetFilter("")
	c.input.Placeholder = "New value for " + item.spec.Label
	c.input.SetValue(current)
	c.input.CursorEnd()
}

func (c *CompactionSettings) stopEditing() {
	c.editing = nil
	c.input.Placeholder = "Type to filter"
	c.input.SetValue("")
}

// commitEdit validates the input for the row being edited and returns the
// write action, or an error report for invalid input.
func (c *CompactionSettings) commitEdit() Action {
	item := c.editing
	if item == nil {
		return nil
	}
	spec := item.spec
	text := strings.TrimSpace(c.input.Value())
	switch spec.Kind {
	case settingKindInt:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil || n < 0 || (spec.MinExclusive && n == 0) {
			return util.ReportError(fmt.Errorf("%s expects a %s integer, got %q", spec.Label, intRequirement(spec), text))
		}
		return ActionSetCompactionOption{Key: spec.Key, Value: n, Message: fmt.Sprintf("%s: %d", spec.Label, n)}
	case settingKindFraction:
		f, err := strconv.ParseFloat(text, 64)
		if err != nil || f <= 0 || f > 1 {
			return util.ReportError(fmt.Errorf("%s expects a number in (0, 1], got %q", spec.Label, text))
		}
		return ActionSetCompactionOption{Key: spec.Key, Value: f, Message: fmt.Sprintf("%s: %s", spec.Label, formatFloat(f))}
	case settingKindFloat:
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return util.ReportError(fmt.Errorf("%s expects a number, got %q", spec.Label, text))
		}
		return ActionSetCompactionOption{Key: spec.Key, Value: f, Message: fmt.Sprintf("%s: %s", spec.Label, formatFloat(f))}
	}
	return nil
}

func intRequirement(spec compactionSettingSpec) string {
	if spec.MinExclusive {
		return "positive"
	}
	return "non-negative"
}

// Cursor returns the cursor position relative to the dialog.
func (c *CompactionSettings) Cursor() *tea.Cursor {
	return InputCursor(c.com.Styles, c.input.Cursor())
}

// Draw implements Dialog.
func (c *CompactionSettings) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := c.com.Styles
	width := max(0, min(compactionSettingsDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()

	c.input.SetWidth(dialogInputTextWidth(t, c.input, innerWidth))

	listTotalHeight := c.list.TotalHeight()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()
	desiredHeight := heightOffset + listTotalHeight
	maxAvailable := area.Dy() - t.Dialog.View.GetVerticalBorderSize()
	height := max(compactionSettingsDialogMinHeight, min(compactionSettingsDialogMaxHeight, desiredHeight, maxAvailable))

	listHeight, listTotalHeight, _ := sizeDialogList(t, c.list, innerWidth, height)

	rc := NewRenderContext(t, width)
	rc.Title = "Compaction Settings"
	if c.editing != nil {
		rc.TitleInfo = t.Dialog.ListItem.InfoFocused.Render("editing " + c.editing.spec.Label)
	}
	inputView := t.Dialog.InputPrompt.Render(c.input.View())
	rc.AddPart(inputView)

	visibleCount := len(c.list.FilteredItems())
	if c.list.Height() >= visibleCount {
		c.list.ScrollToTop()
	} else {
		c.list.ScrollToSelected()
	}

	listView := t.Dialog.List.Height(c.list.Height()).Render(c.list.Render())
	listView = joinScrollbar(t, listView, listHeight, listTotalHeight, listHeight, c.list.Offset())
	rc.AddPart(listView)
	rc.Help = renderDialogHelp(t, &c.help, c, innerWidth)

	view := rc.Render()
	cur := c.Cursor()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp implements help.KeyMap.
func (c *CompactionSettings) ShortHelp() []key.Binding {
	if c.editing != nil {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
			c.keyMap.Cancel,
		}
	}
	return []key.Binding{
		c.keyMap.UpDown,
		c.keyMap.Select,
		c.keyMap.Close,
	}
}

// FullHelp implements help.KeyMap.
func (c *CompactionSettings) FullHelp() [][]key.Binding {
	return [][]key.Binding{c.ShortHelp()}
}
