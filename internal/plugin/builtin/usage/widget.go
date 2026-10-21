package usage

import (
	"strconv"
	"strings"
	"time"

	"kero/internal/plugin/protocol"
	"kero/internal/plugin/sdk"
	"kero/internal/resources"
	accountusage "kero/internal/usage"
	"kero/locales"
)

// widgetTree builds what one pane draws. It is a pure function of the view,
// so a test reads the tree without a process.
func widgetTree(v *view, now time.Time) protocol.Tree {
	short := compact(v.size)
	var children []*sdk.Node
	children = append(children, headRow(v, short))
	if short {
		for _, r := range v.reports {
			children = append(children, compactCard(r))
		}
	} else {
		for _, r := range v.reports {
			children = append(children, accountCard(r, now))
		}
		if len(v.reports) > 0 {
			children = append(children, sdk.Divider("usage-divider"))
		}
	}
	if system := systemSection(v, short); system != nil {
		children = append(children, system)
	}
	if !short && len(v.reports) > 0 {
		children = append(children, sdk.Text("usage-account-note", locales.UsagePluginAccountNote()).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	root := sdk.Column("usage-root", children...)
	if short {
		root.Gap(7).Padding(8)
	} else {
		root.Gap(10).Padding(12)
	}
	return root.Tree()
}

// compact tells whether a pane shows its short form: one row for each
// account, with its bar and its percent.
func compact(size protocol.Size) bool {
	if size.Height > 0 && size.Height < compactHeight {
		return true
	}
	return size.Width > 0 && size.Width < compactWidth
}

// headRow is the title, when the pane was last read and the refresh button.
// The short form has no room for the time, so it says that it is checking
// until an account has been read.
func headRow(v *view, short bool) *sdk.Node {
	head := []*sdk.Node{sdk.Text("usage-title", locales.UsagePluginTitle()).Role(protocol.RoleTitle).Grow(1)}
	if !short {
		if updated := updatedLabel(v.reports, v.loading); updated != "" {
			head = append(head, sdk.Text("usage-updated", updated).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
		}
	} else if checking(v.reports) {
		head = append(head, sdk.Text("usage-checking", locales.UsagePluginChecking()).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	head = append(head, sdk.IconButton("usage-refresh", "refresh"))
	return sdk.Row("usage-head", head...).Gap(8).Align(protocol.AlignCenter)
}

// compactCard is one account in the short form: its name and percent over
// its bar, which is all a small pane has room for.
func compactCard(r accountusage.Report) *sdk.Node {
	return sdk.Column("card-"+r.ID,
		sdk.Row("row-"+r.ID,
			sdk.Text("name-"+r.ID, r.Name).Role(protocol.RoleBody).Lines(1).Grow(1),
			sdk.Text("sum-"+r.ID, summary(r)).Role(protocol.RoleCaption),
		).Align(protocol.AlignSpaceBetween).Gap(6),
		sdk.Progress("bar-"+r.ID, firstUsed(r)/100),
	).Gap(3)
}

// accountCard is one account in the large form: the plan and the summary,
// each window with its used percent and its reset, the balance, and when it
// was last read.
func accountCard(r accountusage.Report, now time.Time) *sdk.Node {
	head := []*sdk.Node{sdk.Text("name-"+r.ID, r.Name).Role(protocol.RoleTitle).Grow(1)}
	if r.Plan != "" {
		head = append(head, sdk.Text("plan-"+r.ID, planName(r.Plan)).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	head = append(head, sdk.Text("sum-"+r.ID, summary(r)).Role(protocol.RoleCaption))
	children := []*sdk.Node{sdk.Row("head-"+r.ID, head...).Gap(8).Align(protocol.AlignCenter)}
	for i, w := range r.Windows {
		id := r.ID + "-" + strconv.Itoa(i)
		children = append(children, sdk.Row("window-"+id,
			sdk.Text("window-label-"+id, windowName(w.Label)).Role(protocol.RoleCaption).Color(protocol.ColorMuted),
			sdk.Progress("bar-"+id, clamp(w.Used)/100).Grow(1),
			sdk.Text("window-used-"+id, locales.UsagePluginUsed(percent(w.Used))).Role(protocol.RoleCaption),
			sdk.Text("reset-"+id, resetLabel(w.Resets, now)).Role(protocol.RoleCaption).Color(protocol.ColorMuted),
		).Gap(8).Align(protocol.AlignCenter))
	}
	if r.Balance != "" {
		children = append(children, sdk.Text("balance-"+r.ID, locales.UsagePluginBalance(r.Balance)).Role(protocol.RoleCaption))
	}
	if r.Error != "" {
		children = append(children, sdk.Text("error-"+r.ID, r.Error).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	} else if r.Checked.IsZero() {
		children = append(children, sdk.Text("wait-"+r.ID, locales.UsagePluginConnect()).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	if help := helpFor(r.ID); help != "" && (r.Checked.IsZero() || r.Error != "") {
		children = append(children, sdk.Text("help-"+r.ID, help).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	if !r.Checked.IsZero() {
		when := r.Checked.Local().Format("Mon 3:04 PM")
		label := locales.UsagePluginUpdated(when)
		if r.Error != "" {
			label = locales.UsagePluginLastSuccessful(when)
		}
		children = append(children, sdk.Text("checked-"+r.ID, label).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	return sdk.Column("card-"+r.ID, children...).Gap(6)
}

// systemSection is this Mac's CPU and memory, with a bar for each. The short
// form keeps the two bars and drops the title and the note.
func systemSection(v *view, short bool) *sdk.Node {
	if v.sample.At.IsZero() {
		if v.sampleErr != "" {
			return sdk.Text("usage-system-error", v.sampleErr).Role(protocol.RoleCaption).Color(protocol.ColorMuted)
		}
		return nil
	}
	var children []*sdk.Node
	if !short {
		children = append(children, sdk.Text("usage-system-title", locales.UsagePluginThisMac()).Role(protocol.RoleTitle))
	}
	children = append(children,
		metric("usage-cpu", locales.UsagePluginCpu(), cpuValue(v.sample), cpuRatio(v.sample)),
		metric("usage-ram", locales.UsagePluginRam(), memoryValue(v.sample), memoryRatio(v.sample)),
	)
	if !short {
		children = append(children, sdk.Text("usage-system-note", locales.UsagePluginAcrossAllApps()).Role(protocol.RoleCaption).Color(protocol.ColorMuted))
	}
	return sdk.Column("usage-system", children...).Gap(5)
}

// metric is one machine reading: its name and value over its bar.
func metric(id, label, value string, ratio float64) *sdk.Node {
	return sdk.Column(id,
		sdk.Row(id+"-row",
			sdk.Text(id+"-label", label).Role(protocol.RoleCaption).Color(protocol.ColorMuted).Grow(1),
			sdk.Text(id+"-value", value).Role(protocol.RoleCaption),
		).Align(protocol.AlignSpaceBetween),
		sdk.Progress(id+"-bar", ratio),
	).Gap(3)
}

// firstUsed is the account's first window, as the short form's bar shows it.
func firstUsed(r accountusage.Report) float64 {
	if len(r.Windows) == 0 {
		return 0
	}
	return clamp(r.Windows[0].Used)
}

func clamp(used float64) float64 {
	if used < 0 {
		return 0
	}
	if used > 100 {
		return 100
	}
	return used
}

// percent is a used percent as the card says it, without a decimal.
func percent(used float64) string {
	return strconv.FormatFloat(clamp(used), 'f', 0, 64)
}

// summary is an account's one-line state: its balance, its first window, or
// what it waits for.
func summary(r accountusage.Report) string {
	if r.Balance != "" {
		return r.Balance
	}
	if len(r.Windows) > 0 && !r.Checked.IsZero() {
		return locales.UsagePluginPercent(percent(r.Windows[0].Used))
	}
	switch {
	case r.Error == "Not connected", r.Error == "Sign in again":
		return locales.UsagePluginConnect()
	case r.Error != "":
		return locales.UsagePluginUnavailable()
	}
	if r.Checked.IsZero() {
		return locales.UsagePluginChecking()
	}
	return locales.UsagePluginConnect()
}

// checking tells whether an account is still waiting for its first reading:
// it has no error to explain the wait. The short form's head says so in place
// of the time the large form shows.
func checking(reports []accountusage.Report) bool {
	for _, r := range reports {
		if r.Checked.IsZero() && r.Error == "" {
			return true
		}
	}
	return false
}

// helpFor is what a provider's card says when it has no reading yet or the
// reading failed: how to sign in, as the Usage tab says it.
func helpFor(id string) string {
	switch id {
	case "claude":
		return locales.UsagePluginHelpClaude()
	case "codex":
		return locales.UsagePluginHelpCodex()
	case "opencode":
		return locales.UsagePluginHelpOpencode()
	case "deepseek":
		return locales.UsagePluginHelpDeepseek()
	}
	return ""
}

// updatedLabel is when the accounts were last read, for the large form's
// head.
func updatedLabel(reports []accountusage.Report, loading bool) string {
	var newest time.Time
	for _, r := range reports {
		if r.Checked.After(newest) {
			newest = r.Checked
		}
	}
	if newest.IsZero() {
		if loading {
			return locales.UsagePluginChecking()
		}
		return ""
	}
	return locales.UsagePluginUpdated(newest.Local().Format("Mon 3:04 PM"))
}

// windowName is a window's name as the card says it, shortly: a week.
func windowName(label string) string {
	if label == "Weekly" {
		return "Week"
	}
	return label
}

// planName is a plan's name as the card shows it: the providers' own are
// lowercase, which reads as a word, so it is capitalized a word at a time.
func planName(plan string) string {
	words := strings.Fields(strings.ReplaceAll(plan, "_", " "))
	for i, w := range words {
		r := []rune(w)
		words[i] = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	return strings.Join(words, " ")
}

// resetLabel is a window's reset as the card says it: the time today, the
// weekday for a later day this week, the date from six days on, or now when
// it is due.
func resetLabel(at, now time.Time) string {
	if at.IsZero() {
		return locales.UsagePluginResetUnavailable()
	}
	if !at.After(now) {
		return locales.UsagePluginResetDue()
	}
	local, now := at.Local(), now.Local()
	if local.Format("2006-01-02") == now.Format("2006-01-02") {
		return locales.UsagePluginResets(local.Format("3:04 PM"))
	}
	if at.Sub(now) < 6*24*time.Hour && local.Weekday() != now.Weekday() {
		return locales.UsagePluginResets(local.Format("Mon"))
	}
	return locales.UsagePluginResets(local.Format("Jan 2"))
}

// cpuValue is the CPU percent, or a dash until the sampler has two readings
// to compare.
func cpuValue(sample resources.Sample) string {
	if sample.CPU == nil {
		return locales.UsagePluginNoValue()
	}
	return locales.UsagePluginCpuValue(percent(*sample.CPU))
}

// cpuRatio is the CPU percent as a bar, from 0 to 1.
func cpuRatio(sample resources.Sample) float64 {
	if sample.CPU == nil {
		return 0
	}
	return clamp(*sample.CPU) / 100
}

// memoryValue is the used and total memory, in GiB.
func memoryValue(sample resources.Sample) string {
	return locales.UsagePluginMemory(gib(sample.MemoryUsed, 1), gib(sample.MemoryTotal, 0))
}

// memoryRatio is the used memory as a bar, from 0 to 1.
func memoryRatio(sample resources.Sample) float64 {
	if sample.MemoryTotal == 0 {
		return 0
	}
	return min(float64(sample.MemoryUsed)/float64(sample.MemoryTotal), 1)
}

func gib(bytes uint64, decimals int) string {
	return strconv.FormatFloat(float64(bytes)/(1<<30), 'f', decimals, 64)
}

// filterReports keeps the accounts that are set up on this Mac. An empty
// installed list means they could not be told apart, so every provider is
// shown.
func filterReports(reports []accountusage.Report, installed []string) []accountusage.Report {
	if len(installed) == 0 {
		return reports
	}
	keep := make(map[string]bool, len(installed))
	for _, id := range installed {
		keep[id] = true
	}
	out := make([]accountusage.Report, 0, len(reports))
	for _, r := range reports {
		if keep[r.ID] {
			out = append(out, r)
		}
	}
	return out
}
